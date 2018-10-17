package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"bazil.org/fuse/fuseutil"
	"github.com/putdotio/go-putio/putio"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
)

const defaultUserAgent = "putiofs - FUSE bridge to Put.io"

// FileSystem is the main object that represents a Put.io filesystem.
type FileSystem struct {
	logger  *Logger
	putio   *putio.Client
	hc      *http.Client
	account putio.AccountInfo
}

var (
	_ fs.FS         = (*FileSystem)(nil)
	_ fs.FSStatfser = (*FileSystem)(nil)
)

// NewFileSystem returns a new Put.io FUSE filesystem.
func NewFileSystem(token string, debug bool) *FileSystem {
	oauthClient := oauth2.NewClient(
		context.Background(),
		oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}),
	)
	client := putio.NewClient(oauthClient)
	client.UserAgent = defaultUserAgent

	return &FileSystem{
		putio:  client,
		hc:     &http.Client{Timeout: time.Hour},
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

func (f *FileSystem) download(ctx context.Context, id int64, offset int64, size int) (io.ReadCloser, error) {
	const useTunnel = true
	u, err := f.putio.Files.URL(ctx, id, useTunnel)
	if err != nil {
		return nil, fmt.Errorf("could not fetch file URL: %v", err)
	}

	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, fmt.Errorf("could not create a new request: %v", err)
	}
	req = req.WithContext(ctx)
	from := strconv.FormatInt(offset, 10)
	to := strconv.FormatInt(offset+int64(size)-1, 10)
	req.Header.Set("Range", fmt.Sprintf("bytes=%v-%v", from, to))

	{
		b, _ := httputil.DumpRequest(req, false)
		f.logger.Debugf("download request dump of %v [offset: %v]:\n%v", id, offset, string(b))
	}

	resp, err := f.hc.Do(req)
	if err != nil {
		return nil, err
	}

	{
		b, _ := httputil.DumpResponse(resp, false)
		f.logger.Debugf("download response dump of %v [offset: %v]:\n%v", id, offset, string(b))

	}
	return resp.Body, nil
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
	f.logger.Debugf("fs.Root()")

	root, err := f.get(nil, 0)
	if err != nil {
		f.logger.Printf("could not fetch root dir: %v", err)
		return nil, fuse.EIO
	}

	account, err := f.putio.Account.Info(context.Background())
	if err != nil {
		f.logger.Debugf("could not fetch account information: %v", err)
		return nil, fuse.EIO
	}
	f.account = account

	return &Dir{
		fs:   f,
		File: &root,
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

	// metadata
	*putio.File
}

var (
	_ fs.Node                = (*Dir)(nil)
	_ fs.NodeMkdirer         = (*Dir)(nil)
	_ fs.NodeCreater         = (*Dir)(nil)
	_ fs.NodeRequestLookuper = (*Dir)(nil)
	_ fs.NodeRemover         = (*Dir)(nil)
	_ fs.HandleReadDirAller  = (*Dir)(nil)
	_ fs.NodeSymlinker       = (*Dir)(nil)
	_ fs.NodeRenamer         = (*Dir)(nil)
)

func (d *Dir) String() string {
	return fmt.Sprintf("<Dir ID: %v Name: %q>", d.ID, d.Name)
}

// Attr implements fs.Node interface. It is called when fetching the inode
// attribute for this directory.
func (d *Dir) Attr(ctx context.Context, attr *fuse.Attr) error {
	d.fs.logger.Debugf("dir.Attr(%q)", d.Name)

	attr.Mode = os.ModeDir | 0755
	attr.Uid = uint32(os.Getuid())
	attr.Gid = uint32(os.Getgid())
	attr.Size = uint64(d.Size)
	return nil
}

// Create implements fs.NodeCreater interface. It is called to create and open
// a new file.
func (d *Dir) Create(ctx context.Context, req *fuse.CreateRequest, resp *fuse.CreateResponse) (fs.Node, fs.Handle, error) {
	d.fs.logger.Debugf("dir.Create(%q)", d.Name)

	u, err := d.fs.putio.Files.Upload(ctx, strings.NewReader(""), req.Name, d.ID)
	if err != nil {
		d.fs.logger.Printf("could not create file on remote: %v", err)
		return nil, nil, fuse.EIO
	}

	f := &File{fs: d.fs, File: u.File}
	h, err := f.newHandle()
	return f, h, err
}

// Mkdir implements fs.NodeMkdirer interface. It is called to create a new
// directory.
func (d *Dir) Mkdir(ctx context.Context, req *fuse.MkdirRequest) (fs.Node, error) {
	d.fs.logger.Debugf("dir.Mkdir(%q)", d.Name)

	files, err := d.fs.list(ctx, d.ID)
	if err != nil {
		d.fs.logger.Printf("could not list directory %q: %v", d, err)
		return nil, fuse.EIO
	}

	for _, file := range files {
		if file.Name == req.Name {
			return nil, fuse.EEXIST
		}
	}

	dir, err := d.fs.putio.Files.CreateFolder(ctx, req.Name, d.ID)
	if err != nil {
		d.fs.logger.Printf("could not create folder: %v", err)
		return nil, fuse.EIO
	}

	return &Dir{
		fs:   d.fs,
		File: &dir,
	}, nil
}

// Lookup implements fs.NodeRequestLookuper. It is called to look up a directory entry by name.
func (d *Dir) Lookup(ctx context.Context, req *fuse.LookupRequest, resp *fuse.LookupResponse) (fs.Node, error) {
	// skip junk files to quiet log noise
	filename := req.Name
	if isJunkFile(filename) {
		return nil, fuse.ENOENT
	}

	d.fs.logger.Debugf("dir.Lookup(%q) in %q", req.Name, d.Name)

	// reserved filename lookups
	switch filename {
	case ".quit":
		d.fs.logger.Fatalf("Shutting down due to request .quit lookup\n")
	case ".stat":
		f, _ := d.fs.get(ctx, d.ID)
		stat, _ := json.MarshalIndent(f, "", "  ")
		return staticFileNode(stat), nil
	case ".account":
		acc, _ := json.MarshalIndent(d.fs.account, "", "  ")
		return staticFileNode(acc), nil
	case ".transfers":
		ts, err := d.fs.putio.Transfers.List(ctx)
		if err != nil {
			d.fs.logger.Printf("could not list transfers: %v", err)
			return nil, fuse.EIO
		}
		return staticFileNode(printTransfersChart(ts)), nil
	}

	files, err := d.fs.list(ctx, d.ID)
	if err != nil {
		d.fs.logger.Printf("could not lookup file %q: %v", d, err)
		return nil, fuse.EIO
	}

	for _, file := range files {
		if file.Name != filename {
			continue
		}

		if file.IsDir() {
			return &Dir{
				fs:   d.fs,
				File: &file,
			}, nil
		}
		return &File{
			fs:   d.fs,
			File: &file,
		}, nil
	}

	return nil, fuse.ENOENT
}

// ReadDirAll implements fs.HandleReadDirAller. it returns the entire contents
// of the directory when the directory is being listed (e.g., with "ls").
func (d *Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	d.fs.logger.Debugf("dir.ReadDirAll(%q)", d.Name)

	files, err := d.fs.list(ctx, d.ID)
	if err != nil {
		d.fs.logger.Printf("could not list directory %q: %v", d, err)
		return nil, fuse.EIO
	}

	var entries []fuse.Dirent
	for _, file := range files {
		var dt fuse.DirentType
		if file.IsDir() {
			dt = fuse.DT_Dir
		} else {
			dt = fuse.DT_File
		}
		entry := fuse.Dirent{
			Name: file.Name,
			Type: dt,
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// Remove implements the fs.NodeRemover interface. It is called to remove the
// entry with the given name from the current directory. The entry to be
// removed may correspond to a file or to a directory.
func (d *Dir) Remove(ctx context.Context, req *fuse.RemoveRequest) error {
	d.fs.logger.Debugf("dir.Remove(%q) in %q", req.Name, d.Name)

	filename := req.Name
	if filename == "/" || filename == "Your Files" {
		return fuse.EIO
	}

	files, err := d.fs.list(ctx, d.ID)
	if err != nil {
		d.fs.logger.Printf("could not list directory %q: %v", d, err)
		return fuse.EIO
	}

	for _, file := range files {
		if file.Name == filename {
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
		d.fs.logger.Debugf("could not convert node to dir")
		return fuse.EIO
	}

	oldname := req.OldName
	newname := req.NewName

	d.fs.logger.Debugf("dir.Rename(old: %q, new: %q)", req.OldName, req.NewName)

	files, err := d.fs.list(ctx, d.ID)
	if err != nil {
		d.fs.logger.Printf("could not read directory %q: %v", d, err)
		return fuse.EIO
	}

	fileid := int64(-1)
	for _, file := range files {
		if file.Name == oldname {
			fileid = file.ID
		}
	}
	if fileid < 0 {
		d.fs.logger.Printf("file not found %q: %v", oldname, err)
		return fuse.ENOENT
	}

	// dst and src directories are the same. just change the filename
	if newdir.ID == d.ID {
		err := d.rename(ctx, fileid, oldname, newname)
		if err != nil {
			d.fs.logger.Printf("could not rename: %v", err)
			return fuse.EIO
		}
	}

	// dst and src directory are different. something definitely moved
	err = d.move(ctx, fileid, newdir.ID, oldname, newname)
	if err != nil {
		d.fs.logger.Printf("could not move: %v", err)
		return fuse.EIO
	}
	return nil
}

func (d *Dir) Symlink(ctx context.Context, req *fuse.SymlinkRequest) (fs.Node, error) {
	d.fs.logger.Debugf("dir.Symlink(src: %q, dst: %q)", req.NewName, req.Target)

	return nil, fuse.ENOTSUP
}

func (d *Dir) rename(ctx context.Context, fileid int64, oldname, newname string) error {
	d.fs.logger.Debugf("dir.Rename(from: %v:%q, to: %q)", fileid, oldname, newname)

	if oldname == newname {
		return nil
	}

	return d.fs.rename(ctx, fileid, newname)
}

func (d *Dir) move(ctx context.Context, fileid int64, parent int64, oldname string, newname string) error {
	d.fs.logger.Debugf("dir.move(from: %v:%q, to: %v:%q)", fileid, oldname, parent, newname)

	err := d.fs.move(ctx, parent, fileid)
	if err != nil {
		d.fs.logger.Printf("could not move file: %v", err)
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

	*putio.File // metadata
}

var (
	_ fs.Node            = (*File)(nil)
	_ fs.NodeOpener      = (*File)(nil)
	_ fs.NodeFsyncer     = (*File)(nil)
	_ fs.NodeGetxattrer  = (*File)(nil)
	_ fs.NodeListxattrer = (*File)(nil)
	_ fs.NodeSetxattrer  = (*File)(nil)
	_ fs.NodeSetattrer   = (*File)(nil)
)

func (f *File) String() string {
	return fmt.Sprintf("<File ID: %v Name: %q Size: %v>", f.ID, f.Name, f.Size)
}

// Attr implements fs.Node interface. It is called when fetching the inode
// attribute for this file.
func (f *File) Attr(ctx context.Context, attr *fuse.Attr) error {
	f.fs.logger.Debugf("file.Attr(%q)", f.Name)

	attr.Mode = 0644
	attr.Uid = uint32(os.Getuid())
	attr.Gid = uint32(os.Getgid())
	attr.Size = uint64(f.Size)
	attr.Ctime = f.CreatedAt.Time
	attr.Mtime = f.CreatedAt.Time
	attr.Crtime = f.CreatedAt.Time
	return nil
}

// Open implements the fs.NodeOpener interface. It is called the first time a
// file is opened by any process. Further opens or FD duplications will reuse
// this handle. When all FDs have been closed, Release() will be called.
func (f *File) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	f.fs.logger.Debugf("file.Open(%q, flags: %v)", f.Name, req.Flags)

	return f.newHandle()
}

// Fsync implements the fs.NodeFsyncer interface. It is called to explicitly
// flush cached data to storage.
func (f *File) Fsync(ctx context.Context, req *fuse.FsyncRequest) error {
	f.fs.logger.Debugf("file.Fsync(%q, flags: %v)", f.Name, req.Flags)

	return fuse.ENOTSUP
}

func (f *File) Setattr(ctx context.Context, req *fuse.SetattrRequest, resp *fuse.SetattrResponse) error {
	f.fs.logger.Debugf("file.Setattr(%q)", f.Name)

	if req.Valid.Size() {
		f.Size = int64(req.Size)
	}

	return nil
}

func (f *File) Getxattr(ctx context.Context, req *fuse.GetxattrRequest, res *fuse.GetxattrResponse) error {
	f.fs.logger.Debugf("file.Getxattr(%q)", f.Name)
	return nil
}

func (f *File) Listxattr(ctx context.Context, req *fuse.ListxattrRequest, res *fuse.ListxattrResponse) error {
	f.fs.logger.Debugf("file.Listxattr(%q)", f.Name)
	return nil
}

func (f *File) Removexattr(ctx context.Context, req *fuse.RemovexattrRequest) error {
	f.fs.logger.Debugf("file.Removexattr(%q)", f.Name)
	return nil
}

func (f *File) Setxattr(ctx context.Context, req *fuse.SetxattrRequest) error {
	f.fs.logger.Debugf("file.Setxattr(%q)", f.Name)
	return nil
}

func (f *File) newHandle() (fs.Handle, error) {
	tmp, err := ioutil.TempFile("", "putiofs-")
	if err != nil {
		f.fs.logger.Printf("could not open: %v", err)
		return nil, fuse.EIO
	}

	f.fs.logger.Debugf("created %q for %v", tmp.Name(), f)

	return &fileHandle{
		f:   f,
		tmp: tmp,
	}, nil
}

type fileHandle struct {
	f *File

	// tmp stores the un-flushed file contents. When the handle is released,
	// content is written to the remote.
	tmp   *os.File
	dirty bool
}

var (
	_ fs.HandleReader   = (*fileHandle)(nil)
	_ fs.HandleWriter   = (*fileHandle)(nil)
	_ fs.HandleFlusher  = (*fileHandle)(nil)
	_ fs.HandleReleaser = (*fileHandle)(nil)
)

// Read implements the fs.HandleReader interface. It is called to handle every
// read request.
func (h *fileHandle) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	h.f.fs.logger.Printf(
		"fileHandle.Read(%q, %v bytes at %v, flags %v)",
		h.f.Name,
		req.Size,
		req.Offset,
		req.Flags,
	)

	if req.Offset >= h.f.Size {
		h.f.fs.logger.Printf("Request offset > actual filesize")
		return nil
	}

	body, err := h.f.fs.download(ctx, h.f.ID, req.Offset, req.Size)
	if err != nil {
		h.f.fs.logger.Printf("could not download %v-%v: %v", h.f.ID, h.f.Name, err)
		return fuse.EIO
	}

	buf := make([]byte, req.Size)
	_, err = io.ReadFull(body, buf)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		err = nil
	}
	if err != nil {
		h.f.fs.logger.Printf("could not read file %q: %v", h.f, err)
		return fuse.EIO
	}

	resp.Data = buf
	return nil
}

// Write implements fs.HandleWriter interface. Write requests to write data
// into the handle at the given offset.
func (h *fileHandle) Write(ctx context.Context, req *fuse.WriteRequest, res *fuse.WriteResponse) error {
	h.f.fs.logger.Printf(
		"fileHandle.Write(%q, %d bytes at %d, flags %v)",
		h.f.Name,
		len(req.Data),
		req.Offset,
		req.Flags,
	)

	if h.tmp == nil {
		h.f.fs.logger.Printf("filehandle.Write() called on filehandle without a tempfile set")
		return fuse.EIO
	}

	n, err := h.tmp.WriteAt(req.Data, req.Offset)
	if err != nil {
		h.f.fs.logger.Printf("fileHandle.Write: %v", err)
		return fuse.EIO
	}
	res.Size = n
	h.dirty = true
	return nil
}

func (h *fileHandle) Flush(ctx context.Context, req *fuse.FlushRequest) error {
	h.f.fs.logger.Debugf("fileHandle.Flush(%q)", h.f.Name)

	if h.tmp == nil {
		h.f.fs.logger.Printf("Flush called on filehandle without a tempfile set")
		return fuse.EIO
	}

	if !h.dirty {
		return nil
	}

	_, err := h.tmp.Seek(0, 0)
	if err != nil {
		h.f.fs.logger.Printf("fileHandle.Flush: %v", err)
		return fuse.EIO
	}

	// remove the file first because Upload will create a new file even though
	// the file exists. that's how Putio works.
	if err := h.f.fs.putio.Files.Delete(ctx, h.f.ID); err != nil {
		h.f.fs.logger.Printf("could not delete file %v: %v", h.f.File, err)
		return fuse.EIO
	}

	u, err := h.f.fs.putio.Files.Upload(ctx, h.tmp, h.f.Name, h.f.ParentID)
	if err != nil {
		h.f.fs.logger.Printf("could not upload: %v", err)
		return fuse.EIO
	}

	if u.File == nil {
		h.f.fs.logger.Printf("could not create new file on remote")
		return fuse.EIO
	}

	*h.f.File = *u.File
	h.dirty = false

	return nil
}

// Release implements the fs.HandleReleaser interface. It is called when all
// file descriptors to the file have been closed.
func (h *fileHandle) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	h.f.fs.logger.Debugf("fileHandle.Release(%q)", h.f.Name)

	h.tmp.Close()
	os.Remove(h.tmp.Name())
	h.tmp = nil
	return nil
}

type staticFileNode string

var (
	_ fs.Node         = (*staticFileNode)(nil)
	_ fs.NodeOpener   = (*staticFileNode)(nil)
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

func (f staticFileNode) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	// bypass page cache for static files
	resp.Flags |= fuse.OpenDirectIO
	return f, nil
}

func (s staticFileNode) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	fuseutil.HandleRead(req, resp, []byte(s))
	return nil
}

func printTransfersChart(transfers []putio.Transfer) string {
	if len(transfers) == 0 {
		return "No transfer found\n"
	}

	var buf bytes.Buffer
	const padding = 3

	w := tabwriter.NewWriter(&buf, 0, 0, padding, ' ', 0)
	fmt.Fprintf(w, "Name\tStatus\t▼\t▲\t\n")
	fmt.Fprintf(w, "----\t------\t-\t-\t\n")
	for _, transfer := range transfers {
		var status string
		var dlSpeed, ulSpeed string
		if transfer.Status == "COMPLETED" {
			status = "✓"
			dlSpeed, ulSpeed = " ", " "
		} else {
			status = fmt.Sprintf("%v/%v", humanizeBytes(uint64(transfer.Downloaded)), humanizeBytes(uint64(transfer.Size)))
			dlSpeed = humanizeBytes(uint64(transfer.DownloadSpeed)) + "/s"
			ulSpeed = humanizeBytes(uint64(transfer.UploadSpeed)) + "/s"
		}

		fmt.Fprintf(w, "%v\t%v\t%v\t%v\t\n", transfer.Name, status, dlSpeed, ulSpeed)
	}
	_ = w.Flush()
	return buf.String()
}

// humanizeBytes produces a human readable representation of an SI size.
// Borrowed from github.com/dustin/go-humanize.
func humanizeBytes(s uint64) string {
	if s < 10 {
		return fmt.Sprintf("%dB", s)
	}
	const base = 1000
	sizes := []string{"B", "kB", "MB", "GB", "TB"}
	e := math.Floor(math.Log(float64(s)) / math.Log(base))
	suffix := sizes[int(e)]
	val := math.Floor(float64(s)/math.Pow(base, e)*10+0.5) / 10
	f := "%.0f%s"
	if val < 10 {
		f = "%.1f%s"
	}
	return fmt.Sprintf(f, val, suffix)
}

var junkFilePrefixes = []string{
	// macOS stuff
	"._",
	".DS_Store",
	".Spotlight-",
	".ql_",
	".hidden",
	".metadata_never_index",
	".nomedia",

	// scm stuff
	".git",
	".hg",
	".bzr",
	".svn",
	"_darcs",

	// misc garbage
	".envrc",     // direnv
	".Trash-",    // nautilus
	".localized", // mpv
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
