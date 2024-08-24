#!/usr/bin/env bash
set -o pipefail

scriptdir=$(dirname "$0")
dir="$scriptdir"
echo "Dir: $dir"
# cd "$dir"
cluster="${1:-$CLUSTER}"
if [ "$cluster" != "$CLUSTER" ]; then
    source ocl $cluster -d
fi
echo "CLUSTER: $CLUSTER"
unset podman

if echo && echo "### start go build" && go build -v && echo "### go build ready" &&
    echo && echo "### start image build" && podman build . | tee build.log; then
    imagesha="$(tail -n 1 <build.log)"
    rm build.log

    echo "tag $imagesha to  default-route-openshift-image-registry.apps.cid-scp0.sf-rz.de/scp-images/cluster-node-resources:latest"
    podman tag "$imagesha" default-route-openshift-image-registry.apps.cid-scp0.sf-rz.de/scp-images/cluster-node-resources:latest
    podman push default-route-openshift-image-registry.apps.cid-scp0.sf-rz.de/scp-images/cluster-node-resources:latest

    for dst in dev-scp0 ppr-scp0 vpt-scp0 pro-scp0; do
        echo '----------------------------------------------------------------------------------------------------------'
        copy-image.sh -v scl=cid-scp0 dcl=$dst sns=scp-images dns=scp-images image=cluster-node-resources:latest
    done

    for dst in dev-scp0 cid-scp0 ppr-scp0 vpt-scp0 pro-scp0; do
    # for dst in cid-scp0; do
        stage="${dst/-scp0/}"
        . ocl $dst "scp-operations-$stage"
        oc delete -f deploy/deploy-"$dst"-cluster-node-resources.yml
        oc apply -f deploy/deploy-"$dst"-cluster-node-resources.yml
        echo '-----------------------------------------------------------------------'
    done
    . ocl cid-scp0 scp-operations-cid
else
    echo "Build failed"
    exit 1
fi
