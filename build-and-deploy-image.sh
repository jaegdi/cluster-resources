#!/usr/bin/env bash
set -o pipefail

scriptdir=$(dirname "$0")
dir="$scriptdir"
echo "Dir: $dir"
# set cluster the the first parameter if given or to the env-Var CLUSTER
cluster="${1:-$CLUSTER}"
if [ "$cluster" != "$CLUSTER" ]; then
    # login into the cluster
    source ocl $cluster -d
fi
echo "CLUSTER: $CLUSTER"
# unset my shell functio for podman, to use the native podman
unset podman

# when go build and podman build are ok, then go on
if echo && echo "### start go build" && go build -v && echo "### go build ready" &&
    echo && echo "### start image build" && podman build . | tee build.log; then

    # get the sha of the fresh buildet image from the build log
    imagesha="$(tail -n 1 <build.log)"
    rm build.log

    # tag and push the image to the registry of the build cluster
    echo "tag $imagesha to  default-route-openshift-image-registry.apps.cid-scp0.sf-rz.de/scp-images/cluster-node-resources:latest"
    podman tag "$imagesha" default-route-openshift-image-registry.apps.cid-scp0.sf-rz.de/scp-images/cluster-node-resources:latest
    podman push default-route-openshift-image-registry.apps.cid-scp0.sf-rz.de/scp-images/cluster-node-resources:latest

    # copy the image to the other clusters
    for dst in dev-scp0 ppr-scp0 vpt-scp0 pro-scp0; do
        echo '----------------------------------------------------------------------------------------------------------'
        copy-image.sh -v scl=cid-scp0 dcl=$dst sns=scp-images dns=scp-images image=cluster-node-resources:latest
    done

    # loop over the stages of our clusters
    for dst in dev-scp0 cid-scp0 ppr-scp0 vpt-scp0 pro-scp0; do
        stage="${dst/-scp0/}"
        # log into the cluster
        . ocl $dst "scp-operations-$stage"
        # delete and deploy
        oc delete -f deploy/deploy-"$dst"-cluster-node-resources.yml
        oc apply -f deploy/deploy-"$dst"-cluster-node-resources.yml
        echo '-----------------------------------------------------------------------'
    done
    # log into the build cluster
    . ocl cid-scp0 scp-operations-cid
else
    echo "Build failed"
    exit 1
fi
