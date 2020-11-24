#!/bin/bash
#
# Push updates that need to be vendored in other repositories.

function show_help() {
    cat <<EOF
push_updates.sh [-h]
push_updates.sh [-b BRANCH] [-n] [-r REPO] [-s SHA] [-t TITLE]

  -b BRANCH
    Set the release branch to be updated. Defaults to "master".

  -h
    Shows this help message.

  -n
    Dry run mode. Do not push or create a pull-request.

  -r REPO
    Update the given repository. May be repeated.
    Defaults to updating all cluster-api-provider repositories:
EOF

    for r in $DEFAULT_REPOS; do
        echo "      $r"
    done

cat <<EOF

  -s SHA
    The SHA from machine-api-operator repository to push.
    Defaults to HEAD of master.

  -t TITLE
    The commit and pull-request title to use. Defaults to
    a message that includes the SHA.

Without any arguments, creates a pull request for each repository to
update the vendored version of machine-api-operator to refer to the
current HEAD commit on the master branch.

EOF
}

DEFAULT_REPOS="
cluster-api-provider-aws
cluster-api-provider-azure
cluster-api-provider-baremetal
cluster-api-provider-gcp
cluster-api-provider-openstack
cluster-api-provider-kubevirt
"
TITLE=""
SHA=""
BRANCH_NAME="master"
DRY_RUN=false

while getopts "b:hnr:s:t:" OPTION; do
    case $OPTION in
        b)
            BRANCH_NAME="$OPTARG"
            ;;
        h)
            show_help
            exit 0
            ;;
        n)
            DRY_RUN=true
            ;;
        r)
            REPOS="$REPOS $OPTARG"
            ;;
        s)
            SHA="$OPTARG"
            ;;
        t)
            TITLE="$OPTARG"
            ;;
    esac
done

if ! which hub 2>/dev/null 1>&2; then
    echo "This tool requires the hub command line app." 1>&2
    echo "Refer to https://github.com/github/hub for installation instructions." 1>&2
    exit 1
fi

REPOS=${REPOS:-${DEFAULT_REPOS}}
TMPDIR=${TMPDIR:-/tmp}
WORKING_DIR=$(mktemp -d $TMPDIR/mao-push-updates.XXXX)
cd $WORKING_DIR
echo "Working in $WORKING_DIR"

hub clone openshift/machine-api-operator

if [ -z "$SHA" ]; then
    echo "Determining SHA for machine-api-operator repository..."
    FULL_SHA=$(cd ./machine-api-operator && git show --format="%H" origin/$BRANCH_NAME)
    SHA=$(cd ./machine-api-operator && git show --format="%h" origin/$BRANCH_NAME)
else
    FULL_SHA=$(cd ./machine-api-operator && git show --format="%H" $SHA)
fi
echo "Updating consumers to $SHA"

if [ -z "$TITLE" ]; then
    TITLE="Update machine-api-operator to ${SHA}"
fi

WORKING_BRANCH_NAME="mao-update-$SHA"
REMOTE_NAME="working"

set -e

function fork_repo () {
    local repo_name="$1"

    if ! hub fork --remote-name $REMOTE_NAME; then
        # Forking failed, so maybe we have another repo with that name
        # forked from a different source. Try adding that as a remote.
        echo "Forking failed, trying a simple clone"
        local github_user=$(git config --get github.user)
        git remote add $REMOTE_NAME git@github.com:${github_user}/${repo_name}.git
        git remote update $REMOTE_NAME
    fi
}

for repo in $REPOS; do
    echo
    echo "Building PR for $repo"
    echo

    hub clone openshift/$repo
    pushd ./$repo

    git checkout origin/$BRANCH_NAME
    git checkout -b $WORKING_BRANCH_NAME

    set -x

    # Use -d option to avoid build errors because we aren't going to
    # build locally.
    go get -d github.com/openshift/machine-api-operator@${FULL_SHA}

    go mod tidy
    go mod vendor
    go mod verify

    set +x

    git add .
    git commit -m "$TITLE"

    PAGER= git show --name-only

    popd
done

if $DRY_RUN; then
    echo "Not pushing to $WORKING_BRANCH_NAME or creating PR."
    exit 0
fi

for repo in $REPOS; do
    echo
    echo "Submitting PR for $repo"
    echo

    pushd ./$repo

    fork_repo $repo
    git push -u $REMOTE_NAME $WORKING_BRANCH_NAME
    hub pull-request --no-edit --base openshift:$BRANCH_NAME

    popd
done
