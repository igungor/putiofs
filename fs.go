package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"bazil.org/fuse/fuseutil"
	"github.com/igungor/go-putio/putio"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
)

const defaultUserAgent = "putiofs - FUSE bridge to Put.io"
const attrValidityDuration = time.Hour

// FileSystem is the main object that represents a Put.io filesystem.
type FileSystem struct {
	logger  *Logger
	putio   *putio.Client
	account putio.AccountInfo
}

var (
	_ fs.FS         = (*FileSystem)(nil)
	_ fs.FSStatfser = (*FileSystem)(nil)
)

// NewFileSystem returns a new Put.io FUSE filesystem.
func NewFileSystem(token string, debug bool) *FileSystem {
	oauthClient := oauth2.NewClient(
		oauth2.NoContext,
		oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}),
	)
	client := putio.NewClient(oauthClient)
	client.UserAgent = defaultUserAgent

	return &FileSystem{
		putio:  client,
		logger: NewLogger("putiofs: ", debug),
	}
}

func (f *FileSystem) list(ctx context.Context, id int64) ([]putio.File, error) {
	files, _, err := f.putio.Files.List(ctx, id)
	return files, err
}

func (f *FileSystem) get(ctx context.Context, id int64) (putio.File, error) {
	return f.putio.Files.Get(ctx, id)
}

func (f *FileSystem) remove(ctx context.Context, id int64) error {
	return f.putio.Files.Delete(ctx, id)
}

func (f *FileSystem) download(ctx context.Context, id int64, offset int64) (io.ReadCloser, error) {
	rangeHeader := http.Header{}
	rangeHeader.Set("Range", fmt.Sprintf("bytes=%v-", strconv.FormatInt(offset, 10)))
	return f.putio.Files.Download(nil, id, true, rangeHeader)
}

func (f *FileSystem) rename(ctx context.Context, id int64, newname string) error {
	return f.putio.Files.Rename(ctx, id, newname)
}

func (f *FileSystem) move(ctx context.Context, parent int64, fileid int64) error {
	return f.putio.Files.Move(ctx, parent, fileid)
}

// Root implements fs.FS interface. It is called once to get the root
// directory inode for the mount point.
func (f *FileSystem) Root() (fs.Node, error) {
	f.logger.Debugf("Root() request\n")

	root, err := f.get(nil, 0)
	if err != nil {
		f.logger.Printf("Root failed: %v\n", err)
		return nil, fuse.EIO
	}

	account, err := f.putio.Account.Info(nil)
	if err != nil {
		f.logger.Debugf("Fetching account info failed: %v\n", err)
		return nil, fuse.EIO
	}
	f.account = account

	return &Dir{
		fs:   f,
		ID:   root.ID,
		Name: root.Filename,
		Size: root.Filesize,
	}, nil
}

// Statfs implements fs.FSStatfser interface.
func (f *FileSystem) Statfs(ctx context.Context, req *fuse.StatfsRequest, resp *fuse.StatfsResponse) error {
	// each block size is 4096 bytes by default.
	const unit = uint64(4096)

	resp.Bsize = uint32(unit)
	resp.Blocks = uint64(f.account.Disk.Size) / unit
	resp.Bavail = uint64(f.account.Disk.Avail) / unit
	resp.Bfree = uint64(f.account.Disk.Avail) / unit

	return nil
}

// Dir is single directory reference in the Put.io filesystem.
type Dir struct {
	fs *FileSystem

	ID   int64
	Name string
	Size int64
}

var (
	_ fs.Node                = (*Dir)(nil)
	_ fs.NodeMkdirer         = (*Dir)(nil)
	_ fs.NodeRequestLookuper = (*Dir)(nil)
	_ fs.NodeRemover         = (*Dir)(nil)
	_ fs.HandleReadDirAller  = (*Dir)(nil)
)

func (d *Dir) String() string {
	return fmt.Sprintf("<Dir ID: %v Name: %q>", d.ID, d.Name)
}

// Attr implements fs.Node interface. It is called when fetching the inode
// attribute for this directory.
func (d *Dir) Attr(ctx context.Context, attr *fuse.Attr) error {
	d.fs.logger.Debugf("Directory stat for %v\n", d)

	attr.Mode = os.ModeDir | 0755
	attr.Uid = uint32(os.Getuid())
	attr.Gid = uint32(os.Getgid())
	attr.Size = uint64(d.Size)
	return nil
}

// Create implements fs.NodeCreater interface. It is called to create and open
// a new file.
func (d *Dir) Create(ctx context.Context, req *fuse.CreateRequest, resp *fuse.CreateResponse) (fs.Node, fs.Handle, error) {
	d.fs.logger.Debugf("File create request for %v\n", d)

	fmt.Println(req.Name)

	u, err := d.fs.putio.Files.Upload(ctx, strings.NewReader(""), req.Name, d.ID)
	if err != nil {
		d.fs.logger.Printf("Upload failed: %v\n", err)
		return nil, nil, fuse.EIO
	}

	// possibly a torrent file is uploaded. torrent files are picked up by the
	// Put.io API and pushed into the transfer queue. Original torrent file is
	// not keeped.
	if u.Transfer != nil {
		return nil, nil, fuse.ENOENT
	}

	if u.File == nil {
		return nil, nil, fuse.EIO
	}

	f := &File{fs: d.fs, File: u.File}

	return f, f, nil
}

// Mkdir implements fs.NodeMkdirer interface. It is called to create a new
// directory.
func (d *Dir) Mkdir(ctx context.Context, req *fuse.MkdirRequest) (fs.Node, error) {
	d.fs.logger.Debugf("Directory mkdir request for %v\n", d)

	name := req.Name

	files, err := d.fs.list(ctx, d.ID)
	if err != nil {
		d.fs.logger.Printf("Listing directory failed for %v: %v\n", d, err)
		return nil, fuse.EIO
	}

	for _, file := range files {
		if file.Filename == name {
			return nil, fuse.EEXIST
		}
	}

	dir, err := d.fs.putio.Files.CreateFolder(ctx, name, d.ID)
	if err != nil {
		d.fs.logger.Printf("Create folder failed: %v\n", err)
		return nil, fuse.EIO
	}

	return &Dir{
		fs:   d.fs,
		ID:   dir.ID,
		Name: dir.Filename,
		Size: dir.Filesize,
	}, nil
}

// Lookup implements fs.NodeRequestLookuper. It is called to look up a directory entry by name.
func (d *Dir) Lookup(ctx context.Context, req *fuse.LookupRequest, resp *fuse.LookupResponse) (fs.Node, error) {
	// skip junk files to quiet log noise
	filename := req.Name
	if isJunkFile(filename) {
		return nil, fuse.ENOENT
	}

	d.fs.logger.Debugf("Directory lookup for %v in %v\n", req.Name, d)

	// reserved filename lookups
	switch filename {
	case ".account":
		acc, _ := json.MarshalIndent(d.fs.account, "", "  ")
		return staticFileNode(acc), nil
	}

	files, err := d.fs.list(ctx, d.ID)
	if err != nil {
		d.fs.logger.Printf("Lookup failed for %v: %v\n", d, err)
		return nil, fuse.EIO
	}

	for _, file := range files {
		if file.Filename == filename {
			if file.IsDir() {
				return &Dir{
					fs:   d.fs,
					ID:   file.ID,
					Name: file.Filename,
					Size: file.Filesize,
				}, nil
			}
			return &File{
				fs:   d.fs,
				File: &file,
			}, nil
		}
	}

	return nil, fuse.ENOENT
}

// ReadDirAll implements fs.HandleReadDirAller. it returns the entire contents
// of the directory when the directory is being listed (e.g., with "ls").
func (d *Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	d.fs.logger.Debugf("Directory listing for %v\n", d)

	files, err := d.fs.list(ctx, d.ID)
	if err != nil {
		d.fs.logger.Printf("Listing directory failed for %v: %v\n", d, err)
		return nil, fuse.EIO
	}

	var entries []fuse.Dirent
	for _, file := range files {
		var entry fuse.Dirent

		var dt fuse.DirentType
		if file.IsDir() {
			dt = fuse.DT_Dir
		} else {
			dt = fuse.DT_File
		}
		entry = fuse.Dirent{
			Name: file.Filename,
			Type: dt,
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// Remove implements the fs.NodeRemover interace. It is called to remove the
// entry with the given name from the current directory. The entry to be
// removed may correspond to a file or to a directory.
func (d *Dir) Remove(ctx context.Context, req *fuse.RemoveRequest) error {
	d.fs.logger.Debugf("Remove request for %v in %v\n", req.Name, d)

	filename := req.Name
	if filename == "/" || filename == "Your Files" {
		return fuse.EIO
	}

	files, err := d.fs.list(ctx, d.ID)
	if err != nil {
		d.fs.logger.Printf("Listing directory failed for %v: %v\n", d, err)
		return fuse.EIO
	}

	for _, file := range files {
		if file.Filename == filename {
			return d.fs.remove(ctx, file.ID)
		}
	}

	return fuse.ENOENT
}

// Rename implements fs.NodeRenamer interface. It's called to rename a file
// from one name to another, possibly in another directory. Renaming either a
// file or directory is allowed. Moving a file/directory to another one is also
// supported.
func (d *Dir) Rename(ctx context.Context, req *fuse.RenameRequest, newDir fs.Node) error {
	newdir, ok := newDir.(*Dir)
	if !ok {
		d.fs.logger.Debugf("Error converting Node to Dir\n")
		return fuse.EIO
	}

	oldname := req.OldName
	newname := req.NewName

	d.fs.logger.Debugf("origdirid: %v, newDirid: %v, old: %v, newname: %v\n", d, newdir, req.OldName, req.NewName)

	files, err := d.fs.list(ctx, d.ID)
	if err != nil {
		d.fs.logger.Printf("Listing directory failed for %v: %v\n", d, err)
		return fuse.EIO
	}

	fileid := int64(-1)
	for _, file := range files {
		if file.Filename == oldname {
			fileid = file.ID
		}
	}
	if fileid < 0 {
		d.fs.logger.Printf("File not found %v: %v\n", oldname, err)
		return fuse.ENOENT
	}

	// dst and src directories are the same. just change the filename
	if newdir.ID == d.ID {
		err := d.rename(ctx, fileid, oldname, newname)
		if err != nil {
			d.fs.logger.Printf("Rename failed: %v\n", err)
			return fuse.EIO
		}
	}

	// dst and src directory are different. something definitely moved
	err = d.move(ctx, fileid, newdir.ID, oldname, newname)
	if err != nil {
		d.fs.logger.Printf("Move failed: %v\n", err)
		return fuse.EIO
	}
	return nil
}

func (d *Dir) rename(ctx context.Context, fileid int64, oldname, newname string) error {
	d.fs.logger.Debugf("Rename request for %v:%v -> %v\n", fileid, oldname, newname)

	if oldname == newname {
		return nil
	}

	return d.fs.rename(ctx, fileid, newname)
}

func (d *Dir) move(ctx context.Context, fileid int64, parent int64, oldname string, newname string) error {
	d.fs.logger.Debugf("Move request for %v:%v -> %v:%v\n", fileid, oldname, parent, newname)

	err := d.fs.move(ctx, parent, fileid)
	if err != nil {
		d.fs.logger.Printf("Error moving file: %v\n", err)
		return fuse.EIO
	}

	// something has moved *and* renamed
	if oldname != newname {
		return d.fs.rename(ctx, fileid, newname)
	}

	return nil
}

// File is single file reference in the Put.io filesystem.
type File struct {
	fs *FileSystem

	// metadata
	*putio.File

	// read offset
	offset int64

	// data
	body io.ReadCloser
}

var (
	_ fs.Node       = (*File)(nil)
	_ fs.NodeOpener = (*File)(nil)

	_ fs.HandleReader   = (*File)(nil)
	_ fs.HandleReleaser = (*File)(nil)
	_ fs.NodeFsyncer    = (*File)(nil)
)

func (f *File) String() string {
	return fmt.Sprintf("<File ID: %v Name: %q Size: %v>", f.ID, f.Filename, f.Filesize)
}

// Attr implements fs.Node interface. It is called when fetching the inode
// attribute for this file.
func (f *File) Attr(ctx context.Context, attr *fuse.Attr) error {
	f.fs.logger.Debugf("File stat for %v\n", f)

	attr.Mode = 0644
	attr.Uid = uint32(os.Getuid())
	attr.Gid = uint32(os.Getgid())
	attr.Size = uint64(f.Filesize)
	attr.Ctime = f.CreatedAt.Time
	attr.Mtime = f.CreatedAt.Time
	attr.Crtime = f.CreatedAt.Time
	return nil
}

// Open implements the fs.NodeOpener interface. It is called the first time a
// file is opened by any process. Further opens or FD duplications will reuse
// this handle. When all FDs have been closed, Release() will be called.
func (f *File) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	f.fs.logger.Debugf("File open request for %v\n", f)

	return f, nil
}

// Release implements the fs.HandleReleaser interface. It is called when all
// file descriptors to the file have been closed.
func (f *File) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	f.fs.logger.Debugf("File Release request")

	f.offset = 0
	if f.body != nil {
		err := f.body.Close()
		if err != nil {
			f.fs.logger.Printf("File release failed: %v\n", err)
		}
	}
	return nil
}

// Read implements the fs.HandleReader interface. It is called to handle every read request.
func (f *File) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	f.fs.logger.Debugf("File Read request. Handle offset: %v, Request (offset: %v size: %v)\n", f.offset, req.Offset, req.Size)

	if req.Offset >= f.Filesize {
		f.fs.logger.Printf("Request offset > actual filesize\n")
		return nil
	}

	var renew bool
	switch {
	case f.body == nil: // initial read
		renew = true
	case f.offset != req.Offset: // seek occurred
		renew = true
		_ = f.body.Close()
	}

	if renew {
		body, err := f.fs.download(nil, f.ID, req.Offset)
		if err != nil {
			f.fs.logger.Printf("Error downloading %v-%v: %v\n", f.ID, f.Filename, err)
			return fuse.EIO
		}
		// reset offset and the body
		f.offset = req.Offset
		f.body = body
	}

	buf := make([]byte, req.Size)
	n, err := io.ReadFull(f.body, buf)
	f.offset += int64(n)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		err = nil
	}
	if err != nil {
		f.fs.logger.Printf("Error reading file %v: %v\n", f, err)
		return fuse.EIO
	}

	resp.Data = buf[:n]
	return nil
}

// Write implements fs.HandleWriter interface. Write requests to write data
// into the handle at the given offset.
func (f *File) Write(ctx context.Context, req *fuse.WriteRequest, resp *fuse.WriteResponse) error {
	f.fs.logger.Debugf("File Write request for %q. Offset: %v\n", f, req.Offset)

	return fuse.ENOTSUP
}

// Fsync implements the fs.NodeFsyncer interface. It is called to explicitly
// flush cached data to storage.
func (f *File) Fsync(ctx context.Context, req *fuse.FsyncRequest) error {
	f.fs.logger.Debugf("Fsync request for %v\n", f)

	return fuse.ENOTSUP
}

type staticFileNode string

var (
	_ fs.Node         = (*staticFileNode)(nil)
	_ fs.HandleReader = (*staticFileNode)(nil)
)

// Attr implements fs.Node interface. It is called when fetching the inode
// attribute for this static file.
func (s staticFileNode) Attr(ctx context.Context, attr *fuse.Attr) error {
	attr.Mode = 0400
	attr.Uid = uint32(os.Getuid())
	attr.Gid = uint32(os.Getgid())
	attr.Size = uint64(len(s))
	return nil
}

func (s staticFileNode) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	fuseutil.HandleRead(req, resp, []byte(s))
	return nil
}

var junkFilePrefixes = []string{
	"._",          // macOS
	".DS_Store",   // macOS
	".Spotlight-", // macOS
	".ql_",        // macOS quicklook
	".hidden",     // macOS
	".metadata_never_index",
	".nomedia",
	".git",
	".envrc",  // direnv
	".Trash-", // nautilus
}

// isJunkFile reports whether the given file path is considered useless. MacOSX
// Finder is looking for a few hidden files per a file stat request. So this is
// used to speed things a bit.
func isJunkFile(abspath string) bool {
	_, filename := filepath.Split(abspath)
	for _, v := range junkFilePrefixes {
		if strings.HasPrefix(filename, v) {
			return true
		}
	}
	return false
}
