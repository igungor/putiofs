# putio  [![Build Status](https://travis-ci.org/putdotio/go-putio.svg?branch=master)](https://travis-ci.org/putdotio/go-putio)

putio is a Go client library for accessing the [Put.io API v2](https://api.put.io/v2/docs).

## Documentation

Available on [GoDoc](http://godoc.org/github.com/putdotio/go-putio/putio)

## Install

```sh
go get -u github.com/putdotio/go-putio/putio"
```

## Usage

```go
package main

import (
        "fmt"
        "log"
        "context"

        "golang.org/x/oauth2"
        "github.com/putdotio/go-putio/putio"
)

func main() {
    oauthToken := "<YOUR-TOKEN-HERE>"
    tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: oauthToken})
    oauthClient := oauth2.NewClient(oauth2.NoContext, tokenSource)

    client := putio.NewClient(oauthClient)

    const rootDir = 0
    root, err := client.Files.Get(context.Background(), rootDir)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(root.Filename)
}
```
