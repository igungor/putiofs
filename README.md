# putiofs

Fuse filesystem for Put.io.

## installation

First you'll need Go. If you're running macOS then you'll also need
[OSXFUSE](https://osxfuse.github.io/).

```sh
go get github.com/igungor/putiofs
```


## run

```sh
mkdir putio
putiofs -token <your-personal-token> putio
```

## license

MIT. See LICENSE.
