default: apply

update:
  aqua up
  aqua update-checksum --prune
  aqua i

build:
  GOFLAGS="-trimpath" go build -ldflags "-s -w -extldflags '-static'"
