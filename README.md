# View Node Resources of Openshift / Kubernets Cluster
This programm generates a table with all nodes of the cluster
For each node the resources for
- physical resources of the node
- sum of requested resources by all pods of the node
- sum of all limit resources definition of all pods of the node
- sum of current used resources of the node
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
