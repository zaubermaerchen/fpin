# fpin

Build with a version injected at build time:

```sh
go build -ldflags "-X main.version=v0.1.0" -o fpin ./cmd/fpin
```
