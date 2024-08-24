#!/usr/bin/env bash

# loop over the stages of our clusters
for c in dev cid ppr vpt pro; do
    # define the cluster-name
    clustername="$c-scp0"
    # log into the cluster with a shell function, can be replaced with "oc login ....."
    . ocl $clustername
    # which serviceaccount is used depends of our clusters
    if [[ $c =~ dev|cid ]]; then
        sa="pipeline"
    else
        sa="scp"
    fi
    # set the namespace to current where the service schoul be executed
    oc project scp-operations-$c
    # get the token secret of the serviceaccount an link it to the serviceaccount
    sec="$(oc get secret|rg scp-token|pc 1)"
    oc secret link scp "$sec"
    # define rolebinding for namespace admin and cluster-reader
    oc adm policy add-cluster-role-to-user cluster-reader -z $sa
    oc adm policy add-cluster-role-to-user admin -z $sa
    # delete and deploy
    oc delete -f deploy-$c-scp0-cluster-node-resources.yml
    oc apply -f deploy-$c-scp0-cluster-node-resources.yml
    echo '-----------------------------------------------------------------------'
done
