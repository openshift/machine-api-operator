#!/bin/sh
cd /tmp
go get github.com/securego/gosec/cmd/gosec
gosec -severity medium --confidence medium -quiet "${@}"
cd -
