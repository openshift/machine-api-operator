#!/bin/bash
#
# Push updates that need to be vendored in other repositories.

function show_help() {
    cat <<EOF
push_updates.sh [-h]
push_updates.sh [-b BRANCH] [-n] [-r REPO] [-s SHA] [-t TITLE]

  -b BRANCH
    Set the release branch to be updated. Defaults to repo's default (master, main, whatever).

  -h
    Shows this help message.

  -n
    Dry run mode. Do not push or create a pull-request.

  -r REPO
    Update the given repository. May be repeated.
    Defaults to updating all machine-api-provider repositories:
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

  -d DESCRIPTION
    Pull request description body. Default is empty.

Without any arguments, creates a pull request for each repository to
update the vendored version of machine-api-operator to refer to the
current HEAD commit on the master branch.

EOF
}

DEFAULT_REPOS="
machine-api-provider-aws
machine-api-provider-azure
machine-api-provider-gcp
machine-api-provider-openstack
machine-api-provider-powervs
machine-api-provider-nutanix
cluster-api-provider-baremetal
cluster-api-provider-ibmcloud
cluster-api-provider-alibaba
"

TITLE=""
SHA=""
BRANCH_NAME=""
PR_DESCRIPTION=""
DRY_RUN=false

while getopts "b:hnr:s:t:d:" OPTION; do
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
        d)
            PR_DESCRIPTION="$OPTARG"
            ;;
    esac
done

if ! which gh 2>/dev/null 1>&2; then
    echo "This tool requires the hub command line app." 1>&2
    echo "Refer to https://github.com/cli/cli for installation instructions." 1>&2
    exit 1
fi

REPOS=${REPOS:-${DEFAULT_REPOS}}
TMPDIR=${TMPDIR:-/tmp}
WORKING_DIR=$(mktemp -d $TMPDIR/mao-push-updates.XXXX)
cd $WORKING_DIR
echo "Working in $WORKING_DIR"

gh repo clone openshift/machine-api-operator

if [ -z "$SHA" ]; then
    echo "Determining SHA for machine-api-operator repository..."

    if [ -z "$BRANCH_NAME" ]; then
      MAO_BRANCH=origin/master
    else
      MAO_BRANCH=origin/$BRANCH_NAME
    fi

    echo "Branch: $MAO_BRANCH"

    FULL_SHA=$(cd ./machine-api-operator && git show --format="%H" $MAO_BRANCH)
    SHA=$(cd ./machine-api-operator && git show --format="%h" $MAO_BRANCH)
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

for repo in $REPOS; do
    echo
    echo "Building PR for $repo"
    echo

    gh repo clone openshift/$repo
    pushd ./$repo

    if [ -z "$BRANCH_NAME" ]; then
      git checkout
    else
      git checkout origin/$BRANCH_NAME
    fi

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

      gh repo fork --remote --remote-name $REMOTE_NAME
      git push -u --force $REMOTE_NAME $WORKING_BRANCH_NAME

      # If branch name is not specified, create pr into repo's default
      if [ -z "$BRANCH_NAME" ]; then
        gh pr create --title="$TITLE" --body="$PR_DESCRIPTION"
      else
        gh pr create --title="$TITLE" --body="$PR_DESCRIPTION" --base openshift:$BRANCH_NAME
      fi

    popd
done
