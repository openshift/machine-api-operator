#!/bin/bash
go mod tidy
go mod vendor
go mod verify

cd ./tools
go mod tidy
go mod vendor
go mod verify
