#!/bin/bash
set -eo pipefail

if [ -z "$1" ]; then
    echo "No build is set" 1>&2
    exit 1
fi

BINARY_NAME="$1"
BINARY_NAME_UBI7="${BINARY_NAME}-ubi7"
IMAGE="${BINARY_NAME}:ubi7"
CONTAINER_NAME="${BINARY_NAME}-container"

mkdir -p dist
# build the linux binary
echo "Build linux binary of $BINARY_NAME"
go build -v -o dist

# build the windows binary
echo "Build windows binary of $BINARY_NAME"
GOOS=windows GOARCH=amd64 go build -v -o dist

# check binary and if it works, then upload to artifactory
if dist/$BINARY_NAME -h >/dev/null; then
    echo "Push to artifactory"

    artifactory-upload.sh -lf=dist/$BINARY_NAME       -tr=scptools-bin-dev-local     -tf="tools/$BINARY_NAME"
    artifactory-upload.sh -lf=dist/$BINARY_NAME       -tr=scptools-bin-dev-local     -tf="ocp-stable-4.16/clients/$BINARY_NAME"

    artifactory-upload.sh -lf=dist/$BINARY_NAME.exe   -tr=scptools-bin-dev-local     -tf="tools/$BINARY_NAME"
    artifactory-upload.sh -lf=dist/$BINARY_NAME.exe   -tr=scptools-bin-dev-local     -tf="ocp-stable-4.16/clients/$BINARY_NAME"

    echo "Copy it to share folder PEWI4124://Daten"
    cp dist/$BINARY_NAME dist/$BINARY_NAME.exe  /gast-drive-d/Daten/
fi

echo
echo
echo "#  B U I L D   I M A G E   T O O L   F O R   U B I 7"
echo "bin in $PWD"

# build ubi7 binary in image
/usr/bin/podman build -t "$IMAGE" -f Dockerfile-ubi7

echo "##########  copy binary from container to local  ##########"
if podman ps -a | rg "$CONTAINER_NAME" >/dev/null; then
    podman rm "$CONTAINER_NAME"
fi
podman create --name "$CONTAINER_NAME" "localhost/$IMAGE"
podman cp "$CONTAINER_NAME":/app/dist/$BINARY_NAME "dist/$BINARY_NAME_UBI7"
podman rm "$CONTAINER_NAME"

artifactory-upload.sh -lf="dist/$BINARY_NAME_UBI7"   -tr=scptools-bin-dev-local   -tf="tools/$BINARY_NAME"
artifactory-upload.sh -lf="dist/$BINARY_NAME_UBI7"   -tr=scptools-bin-dev-local   -tf="ocp-stable-4.16/clients/$BINARY_NAME"

for cl in dev-scp0 cid-scp0 ppr-scp0 vpt-scp0 pro-scp0 pro-scp1; do
    nodepattern=tls-v01
    desthost=$cl-$nodepattern-mgmt
    if [[ $cl =~ dev-scp1-c[12] ]]; then
        clnr=$(echo $cl | cut -d'-' -f3)
        nodepattern=${clnr}t
        desthost=$cl$nodepattern-mgmt
    fi
    if scp dist/$BINARY_NAME_UBI7 $desthost:/tmp/; then
        echo ansible-shell ocp $cl $nodepattern "install /tmp/$BINARY_NAME_UBI7 /usr/local/bin/$BINARY_NAME"
        ansible-shell ocp $cl $nodepattern "install /tmp/$BINARY_NAME_UBI7 /usr/local/bin/$BINARY_NAME"
    fi
done
