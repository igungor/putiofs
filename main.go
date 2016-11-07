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
		token = flag.String("token", "", "personal access token")
		debug = flag.Bool("debug", false, "debug mode")
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

	// create the FUSE connection
	conn, err := fuse.Mount(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	filesys := NewFileSystem(*token, *debug)
	err = fs.Serve(conn, filesys)
	if err != nil {
		log.Fatal(err)
	}

	// check if the mount process has an error to report
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
