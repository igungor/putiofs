package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/igungor/go-putio/putio"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
)

const DefaultUserAgent = "putiofs - FUSE bridge to Put.io"
const AttrValidityDuration = time.Hour

type FileSystem struct {
	putio  *putio.Client
	logger *Logger
}

var (
	_ fs.FS = (*FileSystem)(nil)
)

func NewFileSystem(token string, debug bool) *FileSystem {
	oauthClient := oauth2.NewClient(
		oauth2.NoContext,
		oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}),
	)
	client := putio.NewClient(oauthClient)
	client.UserAgent = DefaultUserAgent

	return &FileSystem{
		putio:  client,
		logger: NewLogger("putiofs: ", debug),
	}
}

func (f *FileSystem) List(ctx context.Context, id int64) ([]putio.File, error) {
	files, _, err := f.putio.Files.List(ctx, id)
	return files, err
}

func (f *FileSystem) Get(ctx context.Context, id int64) (putio.File, error) {
	return f.putio.Files.Get(ctx, id)
}

func (f *FileSystem) Delete(ctx context.Context, id int64) error {
	return f.putio.Files.Delete(ctx, id)
}

func (f *FileSystem) Download(ctx context.Context, id int64, rangeHeader http.Header) (io.ReadCloser, error) {
	return f.putio.Files.Download(ctx, id, true, nil)
}

func (f *FileSystem) Rename(ctx context.Context, id int64, newname string) error {
	return f.putio.Files.Rename(ctx, id, newname)
}

func (f *FileSystem) Move(ctx context.Context, parent int64, fileid int64) error {
	return f.putio.Files.Move(ctx, parent, fileid)
}

func (f *FileSystem) Root() (fs.Node, error) {
	root, err := f.Get(nil, 0)
	if err != nil {
		f.logger.Printf("Root failed: %v\n", err)
		return nil, fuse.ENOENT
	}

	return &Dir{
		fs:   f,
		ID:   root.ID,
		Name: root.Filename,
		Size: root.Filesize,
	}, nil
}

type Dir struct {
	fs *FileSystem

	ID   int64
	Name string
	Size int64
}

var (
	_ fs.Node                = (*Dir)(nil)
	_ fs.NodeRequestLookuper = (*Dir)(nil)
	_ fs.NodeRemover         = (*Dir)(nil)
	_ fs.HandleReadDirAller  = (*Dir)(nil)
)

func (d *Dir) String() string {
	return fmt.Sprintf("<%v - %q>", d.ID, d.Name)
}

func (d *Dir) Attr(ctx context.Context, attr *fuse.Attr) error {
	d.fs.logger.Debugf("Directory stat for %v\n", d)

	attr.Inode = uint64(d.ID)
	attr.Mode = os.ModeDir | 0755
	attr.Size = uint64(d.Size)
	return nil
}

// Lookup looks up a specific entry in the current directory.
func (d *Dir) Lookup(ctx context.Context, req *fuse.LookupRequest, resp *fuse.LookupResponse) (fs.Node, error) {
	// skip junk files to quiet log noise
	filename := req.Name
	if isJunkFile(filename) {
		return nil, fuse.ENOENT
	}

	d.fs.logger.Debugf("Directory lookup for %v in %v\n", req.Name, d)

	files, err := d.fs.List(ctx, d.ID)
	if err != nil {
		d.fs.logger.Printf("Lookup failed for %v: %v\n", d, err)
		return nil, fuse.ENOENT
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

func (d *Dir) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	d.fs.logger.Debugf("Directory listing for %v\n", d)

	files, err := d.fs.List(ctx, d.ID)
	if err != nil {
		d.fs.logger.Printf("Listing directory failed for %v: %v\n", d, err)
		return nil, fuse.ENOENT
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
			Inode: uint64(file.ID),
			Name:  file.Filename,
			Type:  dt,
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// Remove removes the entry with the given name from the current directory. The
// entry to be removed may correspond to a file or to a directory.
func (d *Dir) Remove(ctx context.Context, req *fuse.RemoveRequest) error {
	d.fs.logger.Debugf("Remove request for %v in %v\n", req.Name, d)

	filename := req.Name
	if filename == "/" || filename == "Your Files" {
		return fuse.ENOENT
	}

	files, err := d.fs.List(ctx, d.ID)
	if err != nil {
		d.fs.logger.Printf("Listing directory failed for %v: %v\n", d, err)
		return fuse.ENOENT
	}

	for _, file := range files {
		if file.Filename == filename {
			return d.fs.Delete(ctx, file.ID)
		}
	}

	return fuse.ENOENT
}

func (d *Dir) Rename(ctx context.Context, req *fuse.RenameRequest, newDir fs.Node) error {
	newdir, ok := newDir.(*Dir)
	if !ok {
		d.fs.logger.Debugln("Error converting Node to Dir")
		return fuse.ENOENT
	}

	oldname := req.OldName
	newname := req.NewName

	d.fs.logger.Printf("origdirid: %v, newDirid: %v, old: %v, newname: %v\n", d, newdir, req.OldName, req.NewName)

	files, err := d.fs.List(ctx, d.ID)
	if err != nil {
		d.fs.logger.Printf("Listing directory failed for %v: %v\n", d, err)
		return fuse.ENOENT
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

	// request is to ust change the name
	if newdir.ID == d.ID {
		return d.rename(ctx, fileid, oldname, newname)
	}

	// file/directory moved into another directory
	return d.move(ctx, fileid, newdir.ID, oldname, newname)
}

func (d *Dir) rename(ctx context.Context, fileid int64, oldname, newname string) error {
	d.fs.logger.Debugf("Rename request for %v:%v -> %v\n", fileid, oldname, newname)

	if oldname == newname {
		return nil
	}

	return d.fs.Rename(ctx, fileid, newname)
}

func (d *Dir) move(ctx context.Context, fileid int64, parent int64, oldname string, newname string) error {
	d.fs.logger.Debugf("Move request for %v:%v -> %v:%v\n", fileid, oldname, parent, newname)

	err := d.fs.Move(ctx, parent, fileid)
	if err != nil {
		d.fs.logger.Printf("Error moving file: %v\n", err)
		return fuse.ENOENT
	}

	if oldname != newname {
		return d.fs.Rename(ctx, fileid, newname)
	}

	return nil
}

type File struct {
	fs *FileSystem

	*putio.File
}

var (
	_ fs.Node       = (*File)(nil)
	_ fs.NodeOpener = (*File)(nil)
)

func (f *File) Attr(ctx context.Context, attr *fuse.Attr) error {
	f.fs.logger.Debugf("File stat for %v\n", f)

	attr.Inode = uint64(f.ID)
	attr.Mode = os.ModePerm | 0644
	attr.Size = uint64(f.Filesize)
	attr.Ctime = f.CreatedAt.Time
	return nil
}

func (f *File) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	f.fs.logger.Debugf("File open request for %v\n", f)

	body, err := f.fs.Download(nil, f.ID, nil)
	if err != nil {
		f.fs.logger.Printf("Error downloading %v-%v: %v\n", f.ID, f.Filename, err)
		return nil, fuse.ENOENT
	}

	return &FileHandle{
		fs:   f.fs,
		ID:   f.ID,
		Name: f.Filename,
		body: body,
	}, nil
}

type FileHandle struct {
	fs   *FileSystem
	ID   int64
	Name string
	body io.ReadCloser
}

var (
	_ fs.HandleReader   = (*FileHandle)(nil)
	_ fs.HandleReleaser = (*FileHandle)(nil)
)

func (fh *FileHandle) String() string {
	return fmt.Sprintf("<%v - %q>", fh.ID, fh.Name)
}

func (fh *FileHandle) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	fh.fs.logger.Debugln("FileHandler Read request")

	buf := make([]byte, req.Size)
	n, err := io.ReadFull(fh.body, buf)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		err = nil
	}
	if err != nil {
		fh.fs.logger.Printf("Error reading file %v: %v\n", fh, err)
		return err
	}
	resp.Data = buf[:n]
	return nil
}

func (fh *FileHandle) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	fh.fs.logger.Debugln("FileHandler Release request")

	return fh.body.Close()
}

var junkFilePrefixes = []string{
	"._",
	".DS_Store",
	".Spotlight-",
	".git",
	".hidden",
	".metadata_never_index",
	".nomedia",
	".envrc",
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
