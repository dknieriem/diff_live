# diff_live
Golang wasm clone of the defunct QuickDiff.com tool

## Build

From cmd/wasm, run `GOOS=js GOARCH=wasm go build -o  ../../docroot/diff.wasm`

## Local testing

From cmd/server, run `go run main.go` to spin up a test server at [localhost:9090](http://localhost:9090)

## License

See LICENSE file

diff module translated from [diff-match-patch by NeilFraser](http://code.google.com/p/google-diff-match-patch/)
