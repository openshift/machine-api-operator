all: build
.PHONY: all

# Codegen module needs setting these required variables
CODEGEN_OUTPUT_PACKAGE :=github.com/openshift/machine-api-operator/pkg/generated
CODEGEN_API_PACKAGE :=github.com/openshift/machine-api-operator/pkg/apis
CODEGEN_GROUPS_VERSION :=machineapi:v1alpha1

# Include the library makefile
include $(addprefix ./vendor/github.com/openshift/library-go/alpha-build-machinery/make/, \
	operator.mk \
)

# This will call a macro called "build-image" which will generate image specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - Dockerfile path
# $3 - context directory for image build
# It will generate target "image-$(1)" for builing the image an binding it as a prerequisite to target "images".
$(call build-image,origin-$(GO_PACKAGE),./Dockerfile,.)

# This will call a macro called "add-bindata" which will generate bindata specific targets based on the parameters:
# $0 - macro name
# $1 - target suffix
# $2 - input dirs
# $3 - prefix
# $4 - pkg
# $5 - output
# It will generate targets {update,verify}-bindata-$(1) logically grouping them in unsuffixed versions of these targets
# and also hooked into {update,verify}-generated for broader integration.
$(call add-bindata,v4.0.0,./bindata/v4.0.0/...,bindata,v400_00_assets,pkg/operator/v400_00_assets/bindata.go)

clean:
	$(RM) ./machine-api-operator
.PHONY: clean
