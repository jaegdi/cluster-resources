# View Node Resources of Openshift / Kubernets Cluster
This programm generates a table with all nodes of the cluster and the facts about CPU and RAM.

For each node the resources for

- physical resources of the node   (blue column)
- sum of requested resources by all pods of the node  (green column)
- sum of all limit resources definition of all pods of the node  (red column)
- sum of current used resources of the node  (orange column)

are gathered and display in the table

## Working Modes
### CLI

cluster-resources can be executed as cli. See cluster-resources -h for help

### Webservice stand alone
cluster-resources can be startet as a webservice on the local machine.
The service can be requested with the url

| URL                                              | Description                         |
| ------------------------------------------------ | ----------------------------------- |
| - http://localhost:8080/metrics                  | to show all nodes of the cluster    |
| - http://localhost:8080/metrics?node-type=worker | to show worker nodes of the cluster |
| - http://localhost:8080/metrics?node-type=infra  | to show infra nodes of the cluster  |
| - http://localhost:8080/metrics?node-type=master | to show master nodes of the cluster |


### Webservice as Pod in the Cluster
With the Docker build file a Image can be created, that then can be executed in the cluster
In the deploy folder are examples of deployment files.
In the deployment yaml the service account must be set. The serviceaccount must be admin in the current namespace and cluster-reader

If a table with the node resources is displayed there is also a download link to download the table as an excel file.

__The Webservice tries to get a token from the Serviceaccount scp in namespace scp-operations-<cluster>__

__!!! New createt Serviceaccounts in Openshift 4.16 and above have no token per default.
If der Service print the error in the log:
Error getting token from secret: no secret with 'token' in the name found
then create a secret for the serviceaccoutn scp in ns scp-operations-|cluster>
!!!__
```bash
oc project scp-operations-dev # or cid, ppr, pro or oc project scp-ops-central for pro-scp1
oc create secret generic scp-token --from-literal=token="$(oc create token scp --duration=$((24 * 60 * 365 * 3))m)"
oc secret link scp scp-token
```
