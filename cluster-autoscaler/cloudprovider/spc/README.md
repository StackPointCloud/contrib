

NodeGroup is the entity that must implement _DeleteNodes([]*kube_api.Node) error_

The stackpointio api call is DELETE /orgs/{orgID}/clusters/{clusterID}/nodes/{nodeID}

We've got the api token, the cluster id and the organization id already, so we
need to be able to get the node ID from the kubernetes api Node object

kubernetes knows things like:

metadata:
  labels:
    kubernetes.io/hostname: 172.23.1.23
    stackpoint.io/role: worker
  name: ip-172-23-1-23.us-west-2.compute.internal
  selfLink: /api/v1/nodes/ip-172-23-1-23.us-west-2.compute.internal
spec:
  addresses:
  - address: 172.23.1.23
    type: InternalIP


Whereas stackpoint knows

{
    "pk": 1032,
    "name": "",
    "cluster": 504,
    "instance_id": "spcvd7ah21-worker-2",
    "role": "worker",
    "private_ip": "172.23.1.23",
    "public_ip": "52.37.42.201",
    "platform": "coreos",
    "image": "ami-06af7f66",
    "location": "us-west-2:us-west-2a",
    "size": "t2.medium",
    "state": "running",
    "created": "2016-09-27T22:09:57.091286Z",
    "updated": "2016-09-27T22:09:57.091303Z"
 }


post to create auth token:

http://localhost:8000/userprofile/ntfrnzn/add_token
  {"name":"spcvd7ah21"}
response:
 {"token_id":"2da9683dea","token":"a12f59e2788386a302c5d199b5506725dc85692a880aed3303c5b6b195164a94"}

auth token should be created beforehand and passed in.  https://github.com/StackPointCloud/quarter-master-rest/issues/161

http://kubernetes.io/docs/user-guide/secrets/#security-properties



```
18:49:40.699288       1 cluster.go:100] Fast evaluation: node ip-172-23-1-164.us-west-2.compute.internal may be removed
I1012 18:49:40.699308       1 cluster_autoscaler.go:306] ip-172-23-1-164.us-west-2.compute.internal is unneeded since 2016-10-12 18:39:30.650766071 +0000 UTC duration 10m10.048535482s
I1012 18:49:40.699315       1 cluster_autoscaler.go:311] Starting scale down
I1012 18:49:40.699321       1 scale_down.go:123] ip-172-23-1-164.us-west-2.compute.internal was unneeded for 10m10.048551892s
I1012 18:49:40.699325       1 scale_down.go:136] Skipping ip-172-23-1-164.us-west-2.compute.internal - no node group config
I1012 18:49:40.699330       1 scale_down.go:155] No candidates for scale down
```


AutoScaling and Error handling

We have a problem when adding new nodes and we encounter a failure along the way.  The autoscaler cannot do any specific response handling, it only can receive a value of success or failure when attempting to increase the size of the cluster.  Unfortunately, many of the error conditions in k8s_build simply leave the node in a draft or provisioning state instead of marking it failed.  The client has no way of detecting the failure

One of the errors is early-committing of pods.  It can happen that the scheduler aggresively puts pods onto a new node before we have completely configured it.  Because we run flannel as a pod, we can end up not being able to start flannel because of resource limits

Oct 14 23:39:23 spci19qi6h-worker-7 kubelet[4199]: I1014 23:39:23.516651    4199 kubelet.go:2201] Predicate failed on Pod: flannel-172.23.1.17_kube-system(df3741555aee780230538b89b20b88ac7), for reason: Node didn't have enough resource: cpu, requested: 100, used: 2000, capacity: 2000


---

Current status -- confirmed that the node enters the cluster at the completion of the kubernetes/node ansible role

It may take tens of seconds for the scheduler to begin scheduling pods onto it, but it also takes tens of seconds to get flannel reconfigured.

These are the ansible roles run on the node:

- adduser
- kubernetes/preinstall
- etcd
- { role: docker, when: ansible_os_family != "CoreOS" }
- kubernetes/node
- network_plugin

Answer -- mark it as unschedulable when it's created, call `kubectl uncordon` later

----

Need to make sure that etcd nodes are not deleted.  How?

We get this sort of statement in the log,

I1019 23:20:42.780395       1 cluster.go:75] Fast evaluation: node 10.136.30.169 cannot be removed: non-deamons set, non-mirrored, kube-system pod present: kube-dns-v19-4216573841-qmecw
I1019 23:20:42.780417       1 cluster.go:66] Fast evaluation: 10.136.30.168 for removal
I1019 23:20:42.781345       1 cluster.go:75] Fast evaluation: node 10.136.30.168 cannot be removed: non-deamons set, non-mirrored, kube-system pod present: kubernetes-dashboard-1655269645-y53um
I1019 23:20:42.781365       1 cluster.go:66] Fast evaluation: 10.136.30.167 for removal
I1019 23:20:42.781514       1 cluster.go:75] Fast evaluation: node 10.136.30.167 cannot be removed: non-deamons set, non-mirrored, kube-system pod present: test-2601602450-64i17
I1019 23:20:42.781527       1 cluster.go:66] Fast evaluation: 10.136.15.23 for removal
I1019 23:20:42.781591       1 cluster.go:75] Fast evaluation: node 10.136.15.23 cannot be removed: non-deamons set, non-mirrored, kube-system pod present: influxdb-grafana-gjnwi

which is a list of the nodes that cannot be removed.  The check looks at the pods that are running
on a node and determines whether or not they can be relocated.  The commandline flags are

var (
	skipNodesWithSystemPods = flag.Bool("skip-nodes-with-system-pods", true,
		"If true cluster autoscaler will never delete nodes with pods from kube-system (except for DeamonSet "+
			"or mirror pods)")
	skipNodesWithLocalStorage = flag.Bool("skip-nodes-with-local-storage", true,
		"If true cluster autoscaler will never delete nodes with pods with local storage, e.g. EmptyDir or HostPath")
)

which means that if there's pod in kube-system running on that node, it will never be moved.  
This is not future-proof solution, since replicated pods (in a deployment) should
be perfectly safe to move even if they are in kube-system.

And if those kube-system deployments are removed (dashboard, heapster, influxdb)
then the node becomes fair game

The danger for us is that we have etcd running outside of kubernetes and there's
way for kubernetes to know that it's a special host which should not be touched.

We could error-out when DeleteNode is called, but that is rather late.  The scheduling
decisions have been made long before then.

We can survive losing one node ...

```
core@master ~ $ etcdctl cluster-health
member 321d1b88639abbe4 is healthy: got healthy result from http://10.136.30.169:2379
member 3261dd0f41550421 is healthy: got healthy result from http://10.136.30.167:2379
failed to check the health of member ded2f56e9e4b4dcc on http://10.136.30.168:2379: Get http://10.136.30.168:2379/health: dial tcp 10.136.30.168:2379: i/o timeout
member ded2f56e9e4b4dcc is unreachable: [http://10.136.30.168:2379] are all unreachable
cluster is healthy
```
----

I suggest that the initial nodes not belong to an autoscale group at all.  
At cluster request time, we can ask the user whether they want to create
an autoscaling group, and then define it with a group name. Any member
of the group will be created by the autoscaler, and will be tagged with
"stackpoint.io/autoscalegroup=A0" with "A0" as the default group name

The stackpoint api must also be aware that the node belongs to a group OR
is of a particular type OR has a particular tag.  Any of those will be sufficient
for the builder or for the autoscaler to manage.

This will require that the add_node request take some kind of label parameter.  
And also that it return characteristics of the node that will be created.


---

Autoscaler cannot deal with a group of size zero because it needs an existing
node in order to create a template and estimate the sizes

So, we fake it.  Recognize that there's no template and set the increase size to 1.
After that node is created, it can serve as the template for further additions

----  Debug level=6 output looks like:

I1021 21:03:08.151046       1 spc_manager.go:121] Updating cluster info, organizationID 6, clusterID 810
I1021 21:03:08.225283       1 scale_up.go:52] Pod default/load-1953665481-pbjgp is unschedulable
I1021 21:03:08.225317       1 scale_up.go:52] Pod default/load-1953665481-09u3q is unschedulable
I1021 21:03:08.225331       1 spc_cloud_provider.go:236] Error node not manageable i-a158bcba, can't get nodeGroupName
I1021 21:03:08.225361       1 spc_cloud_provider.go:236] Error node not manageable i-b258bca9, can't get nodeGroupName
I1021 21:03:08.225372       1 spc_cloud_provider.go:236] Error node not manageable i-ff58bce4, can't get nodeGroupName
I1021 21:03:08.225382       1 utils.go:182] NodeGroup autoscaling was omitted from initial count
I1021 21:03:08.225391       1 utils.go:184] Patching in information for zero-size node group autoscaling
I1021 21:03:08.225401       1 spc_manager.go:121] Updating cluster info, organizationID 6, clusterID 810
I1021 21:03:08.260216       1 predicates.go:79] CheckPredicates got a dummy NodeInfo with no node, returning nil
I1021 21:03:08.260249       1 predicates.go:79] CheckPredicates got a dummy NodeInfo with no node, returning nil
I1021 21:03:08.260264       1 scale_up.go:105] Best option to resize: autoscaling
E1021 21:03:08.260277       1 scale_up.go:116] got dummy nodeInfo, continuing with default estimate=1
I1021 21:03:08.260285       1 spc_manager.go:121] Updating cluster info, organizationID 6, clusterID 810
I1021 21:03:08.292605       1 scale_up.go:142] Setting autoscaling size to 1
I1021 21:03:08.292653       1 spc_manager.go:121] Updating cluster info, organizationID 6, clusterID 810
I1021 21:03:08.632168       1 spc_manager.go:172] AddNodes response {instance_id: spcumpf7g5-worker-5, state: draft}

...

I1021 21:18:07.984342       1 scale_up.go:71] Skipping node group autoscaling - max size reached



------

I1024 22:07:49.148117       1 scale_up.go:105] Best option to resize: autoscaling
I1024 22:07:49.148177       1 scale_up.go:121] Resources needed:
CPU: 96
Mem: 1536Mi
I1024 22:07:49.148191       1 scale_up.go:122] Needed nodes according to:
CPU: 48
Mem: 1
Pods: 1
I1024 22:07:49.148202       1 scale_up.go:123] Estimated 48 nodes needed in autoscaling
I1024 22:07:49.193536       1 scale_up.go:132] Capping size to MAX (8)
I1024 22:07:49.193622       1 scale_up.go:144] Setting autoscaling size to 8

---------
