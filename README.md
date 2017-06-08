# putiofs

Fuse filesystem for Put.io.

## installation

First you'll need [Go version 1.7 or newer](https://golang.org/dl/). If you're running macOS then you'll also need
[OSXFUSE](https://osxfuse.github.io/).

```sh
go get -u github.com/igungor/putiofs
```

## run

```sh
mkdir putio
putiofs -token <your-personal-token> putio
```

## easter eggs

* read `.transfers` pseudo file in any directory

```sh
cat .transfers
```

* read `.account` pseudo file in any directory

```sh
cat .account
```

## license

MIT. See LICENSE.
