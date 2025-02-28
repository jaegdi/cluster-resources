#!/usr/bin/env bash
destination="${1:-dev-scp0 cid-scp0 ppr-scp0 vpt-scp0 pro-scp0 pro-scp1}"
echo "destinations: $destination"
sleep 1
# copy the image to the other clusters
for dst in $destination; do
    echo '----------------------------------------------------------------------------------------------------------'
    copy-image.sh -v -sc=cid-scp0 -dc=$dst -sn=scp-images -dn=scp-images -i=cluster-node-resources:latest
done

# loop over the stages of our clusters
for dst in $destination; do
    stage="${dst/-scp[0-9]/}"
    case $dst in
        pro-scp1)
            ns="scp-ops-central"
            ;;
        *)
            ns="scp-operations-$stage"
            ;;
    esac
    # log into the cluster
    . ocl $dst "$ns"
    # delete and deploy
    oc delete -f deploy/deploy-"$dst"-cluster-node-resources.yml
    oc apply -f deploy/deploy-"$dst"-cluster-node-resources.yml
    echo '-----------------------------------------------------------------------'
done
# log into the build cluster
. ocl cid-scp0 scp-operations-cid
