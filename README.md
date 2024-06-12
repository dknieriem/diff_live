# diff_live

Golang wasm clone of the defunct QuickDiff.com tool

## License

See LICENSE file

diff module translated from [diff-match-patch by NeilFraser](http://code.google.com/p/google-diff-match-patch/)

## Build

`go build github.com/dknieriem/diff_live/cmd/wasm/diff && GOOS=js GOARCH=wasm go build -o docroot/diff.wasm ./cmd/wasm`

## Test

`go build github.com/dknieriem/diff_live/cmd/wasm/diff && GOOS=js GOARCH=wasm go build -o docroot/diff.wasm ./cmd/wasm && go run ./cmd/server`
