for c in dev cid ppr vpt pro; do
    . ocl $c-scp0
    if [[ c =~ dev|cid ]]; then
        sa="pipeline"
    else
        sa="scp"
    fi
    oc project scp-operations-$c
    sec="$(oc get secret|rg scp-token|pc 1)"
    oc secret link scp $sec
    oc adm policy add-cluster-role-to-user cluster-reader -z $sa
    oc adm policy add-cluster-role-to-user admin -z $sa
    oc delete -f deploy-$c-scp0-cluster-node-resources.yml
    oc apply -f deploy-$c-scp0-cluster-node-resources.yml
    echo '-----------------------------------------------------------------------'
done
