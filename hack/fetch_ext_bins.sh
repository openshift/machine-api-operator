#!/usr/bin/env bash
# Copyright 2018 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

# Enable tracing in this script off by setting the TRACE variable in your
# environment to any value:
#
# $ TRACE=1 test.sh
TRACE=${TRACE:-""}
if [ -n "$TRACE" ]; then
  set -x
fi

k8s_version=1.18.8
etcd_version=3.4.10
goarch=amd64
goos="unknown"

if [[ "$OSTYPE" == "linux"* ]]; then
  goos="linux"
elif [[ "$OSTYPE" == "darwin"* ]]; then
  goos="darwin"
fi

if [[ "$goos" == "unknown" ]]; then
  echo "OS '$OSTYPE' not supported. Aborting." >&2
  exit 1
fi

# Turn colors in this script off by setting the NO_COLOR variable in your
# environment to any value:
#
# $ NO_COLOR=1 test.sh
NO_COLOR=${NO_COLOR:-""}
if [ -z "$NO_COLOR" ]; then
  header=$'\e[1;33m'
  reset=$'\e[0m'
else
  header=''
  reset=''
fi

function header_text {
  echo "$header$*$reset"
}

rc=0
tmp_root=/tmp

kb_root_dir=$tmp_root/kubebuilder
kb_orig=$(pwd)

# Skip fetching and untaring the tools by setting the SKIP_FETCH_TOOLS variable
# in your environment to any value:
#
# $ SKIP_FETCH_TOOLS=1 ./fetch_ext_bins.sh
#
# If you skip fetching tools, this script will use the tools already on your
# machine, but rebuild the kubebuilder and kubebuilder-bin binaries.
SKIP_FETCH_TOOLS=${SKIP_FETCH_TOOLS:-""}

function prepare_staging_dir {
  header_text "preparing staging dir"

  if [ -z "$SKIP_FETCH_TOOLS" ]; then
    rm -rf "$kb_root_dir"
  else
    rm -f "$kb_root_dir/kubebuilder/bin/kubebuilder"
    rm -f "$kb_root_dir/kubebuilder/bin/kubebuilder-gen"
    rm -f "$kb_root_dir/kubebuilder/bin/vendor.tar.gz"
  fi
}

# fetch k8s API gen tools and make it available under kb_root_dir/bin.
function fetch_tools {
  if [ -n "$SKIP_FETCH_TOOLS" ]; then
    return 0
  fi

  header_text "fetching tools"

  mkdir -p "${kb_root_dir}/bin"

  k8s_download_root="https://dl.k8s.io/v${k8s_version}/bin/${goos}/${goarch}"

  curl -fsL "${k8s_download_root}/kubectl" -o "${kb_root_dir}/bin/kubectl"
  chmod +x "${kb_root_dir}/bin/kubectl"

  curl -fsL "${k8s_download_root}/kube-apiserver" -o "${kb_root_dir}/bin/kube-apiserver"
  chmod +x "${kb_root_dir}/bin/kube-apiserver"

  etcd_os_version="etcd-v${etcd_version}-${goos}-${goarch}"
  etcd_archive="${etcd_os_version}.tar.gz"
  etcd_download_url="https://github.com/etcd-io/etcd/releases/download/v${etcd_version}/${etcd_archive}"
  curl -fsL "${etcd_download_url}" -o "${kb_root_dir}/${etcd_archive}"

  tar -zvxf "${kb_root_dir}/${etcd_archive}" -o "${etcd_os_version}/etcd"
  mv "${etcd_os_version}/etcd" "${kb_root_dir}/bin/etcd"
  rm "${kb_root_dir}/${etcd_archive}"
  rm -rf "${etcd_os_version}"
}

function setup_envs {
  header_text "setting up env vars"

  # Setup env vars
  export PATH=/tmp/kubebuilder/bin:$PATH
  export TEST_ASSET_KUBECTL=/tmp/kubebuilder/bin/kubectl
  export TEST_ASSET_KUBE_APISERVER=/tmp/kubebuilder/bin/kube-apiserver
  export TEST_ASSET_ETCD=/tmp/kubebuilder/bin/etcd
}
