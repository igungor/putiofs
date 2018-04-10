package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
)

func main() {
	log.SetFlags(0)

	// flags
	var (
		token    = flag.String("token", "", "personal access token")
		debug    = flag.Bool("debug", false, "debug mode")
		readonly = flag.Bool("readonly", false, "mount filesystem read-only")
	)
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	if *token == "" {
		flag.Usage()
		os.Exit(2)
	}

	// NoAppleDouble makes OSXFUSE disallow files with names used by OS X to
	// store extended attributes on file systems that do not support them
	// natively.
	mountOpts := []fuse.MountOption{
		fuse.NoAppleDouble(),
		fuse.NoAppleXattr(),
	}
	if *readonly {
		mountOpts = append(mountOpts, fuse.ReadOnly())
	}

	conn, err := fuse.Mount(flag.Arg(0), mountOpts...)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	filesys := NewFileSystem(*token, *debug)
	err = fs.Serve(conn, filesys)
	if err != nil {
		log.Fatal(err)
	}

	<-conn.Ready
	if err := conn.MountError; err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage of putiofs:")
	fmt.Fprintln(os.Stderr, "putiofs -token <YOUR TOKEN> <mountpoint>")
	flag.PrintDefaults()
}
