# frestive-pricecheck

Small lightweight program to allow customer to check item price in a retail store by scanning the barcode.
Meant to be compiled and embedded into an ARM-based linux board like the C.H.I.P.
Works with a Barcode Scanner and VFD Line Display for the smallest footprint possible.

### Building

    go get github.com/tarm/serial
    go get github.com/heri16/frestive-pricecheck
    env GOOS=linux GOARCH=arm go build -v src/github.com/heri16/frestive-pricecheck/pricecheck.go
    