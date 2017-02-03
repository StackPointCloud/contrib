/*
Copyright 2016 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package spc

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/golang/glog"
	apimachinery "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/contrib/cluster-autoscaler/cloudprovider"
	apiv1 "k8s.io/kubernetes/pkg/api/v1"
)

type SpcNodeClass struct {
	Type string
	// other specification here
}

// SpcNodeGroup implements NodeGroup
type SpcNodeGroup struct {
	id        string
	Class     SpcNodeClass
	maxSize   int
	minSize   int
	k8sclient *kubernetes.Clientset
	manager   *NodeManager
}

// MaxSize returns maximum size of the node group.
func (sng SpcNodeGroup) MaxSize() int {
	return sng.maxSize
}

// MinSize returns minimum size of the node group.
func (sng SpcNodeGroup) MinSize() int {
	return sng.minSize
}

// // Size returns the current size of the node group.
// func (sng *SpcNodeGroup) Size() int {
// 	return sng.currentSize
// }

// TargetSize returns the current target size of the node group. It is possible that the
// number of nodes in Kubernetes is different at the moment but should be equal
// to Size() once everything stabilizes (new nodes finish startup and registration or
// removed nodes are deleted completely)
func (sng SpcNodeGroup) TargetSize() (int, error) {

	sng.manager.Update()
	count := 0

	for _, node := range sng.manager.nodes {
		if node.Group == sng.Id() && node.State == "running" {
			count++
		}
	}
	return count, nil
}

// IncreaseSize increases the size of the node group. To delete a node you need
// to explicitly name it and use DeleteNode. This function should wait until
// node group size is updated.
func (sng SpcNodeGroup) IncreaseSize(delta int) error {
	_, err := sng.manager.IncreaseSize(delta, sng.Class.Type, sng.Id())
	return err
}

// DecreaseTargetSize decreases the target size of the node group. This function
// doesn't permit to delete any existing node and can be used only to reduce the
// request for new nodes that have not been yet fulfilled. Delta should be negative.
func (sng SpcNodeGroup) DecreaseTargetSize(delta int) error {
	if delta >= 0 {
		return fmt.Errorf("size decrease must be negative")
	}
	return nil
}

// DeleteNodes deletes nodes from this node group. Error is returned either on
// failure or if the given node doesn't belong to this node group. This function
// should wait until node group size is updated.
func (sng SpcNodeGroup) DeleteNodes(nodes []*apiv1.Node) error {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		instanceID, err := instanceIDForNode(node)
		if err != nil {
			glog.V(2).Info(err.Error())
		} else {
			ids = append(ids, instanceID)
		}
	}
	_, err := sng.manager.DeleteNodes(ids)
	return err
}

func labelValueForNode(key string, node *apiv1.Node) (string, error) {
	labels := node.ObjectMeta.Labels
	value, ok := labels[key]
	if !ok {
		var errorResp error
		hostname, reallyOK := labels["kubernetes.io/hostname"]
		if reallyOK {
			errorResp = fmt.Errorf("Unable to find label [%s] for node [%s]", key, hostname)
		} else {
			errorResp = fmt.Errorf("Unable to find label [%s] for a nodes without 'kubernetes.io/hostname' label", key)
		}
		return "", errorResp
	}
	return value, nil
}

func instanceIDForNode(node *apiv1.Node) (string, error) {
	return labelValueForNode("stackpoint.io/instance_id", node)
}

func nodeGroupNameForNode(node *apiv1.Node) (string, error) {
	return labelValueForNode("stackpoint.io/node_group", node)
}

// Id returns an unique identifier of the node group.
func (sng SpcNodeGroup) Id() string {
	return sng.id
}

// Debug returns a string containing all information regarding this node group.
func (sng SpcNodeGroup) Debug() string {
	var msg string
	target, err := sng.TargetSize()
	if err != nil {
		msg = err.Error()
	}
	return fmt.Sprintf("%s (%d:%d) (%d) %s", sng.Id(), sng.MinSize(), sng.MaxSize(), target, msg)
}

// Nodes returns a list of all nodes that belong to this node group.
// return value is a set of ... strings
// for gce, this is "fmt.Sprintf("gce://%s/%s/%s", project, zone, name))"
// for aws, this is Instance.InstanceId, something like "i-04a211e5f5c755e64"
// for azure, this is "azure:////" + fixEndiannessUUID(string(strings.ToUpper(*instance.VirtualMachineScaleSetVMProperties.VMID)))""
func (sng SpcNodeGroup) Nodes() ([]string, error) {
	//var instanceIDs []string
	var names []string
	instanceIDs, err := sng.manager.NodesForGroupID(sng.Id())
	if err != nil {
		return names, err
	}
	for _, instanceID := range instanceIDs {
		labelSelection := "stackpoint.io/instance_id=" + instanceID
		glog.V(5).Infof("Querying nodes for label %s", labelSelection)
		options := apimachinery.ListOptions{LabelSelector: labelSelection}
		nodeList, err := sng.k8sclient.CoreV1().Nodes().List(options)
		if err != nil {
			glog.Error(err)
			break
		}
		glog.V(5).Infof("Retrieved %d nodes for node query", len(nodeList.Items))
		if len(nodeList.Items) > 0 {
			providerID := nodeList.Items[0].Spec.ProviderID
			names = append(names, providerID)
		}
	}
	return names, nil
}

// SpcCloudProvider implements CloudProvider
type SpcCloudProvider struct {
	spcClient  *StackpointClusterClient
	spcManager *NodeManager
	k8sclient  *kubernetes.Clientset
	nodeGroups []*SpcNodeGroup
}

// BuildSpcCloudProvider builds CloudProvider implementation for stackpointio.
func BuildSpcCloudProvider(spcClient *StackpointClusterClient, specs []string) (*SpcCloudProvider, error) {
	if spcClient == nil {
		return nil, fmt.Errorf("ClusterClient is nil")
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("No node group is specified")
	}
	manager := CreateNodeManager(spcClient)

	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("Cannot get the in-cluster config")
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("Cannot create the in-cluster client")
	}

	spc := &SpcCloudProvider{
		spcClient:  spcClient,
		spcManager: &manager,
		k8sclient:  clientset,
		nodeGroups: make([]*SpcNodeGroup, 0),
	}

	for _, spec := range specs {
		if _, err := spc.addNodeGroup(spec); err != nil {
			return nil, err
		}
	}
	return spc, nil
}

// addNodeGroup adds node group defined in string spec. Format:
// minNodes:maxNodes:typeofNode  Returns the name of the nodeGroup
func (spc *SpcCloudProvider) addNodeGroup(spec string) (string, error) {
	glog.V(5).Infof("Node group specification: %s", spec)
	minSize, maxSize, nodeType, id, err := tokenizeNodeGroupSpec(spec)
	if err != nil {
		return "", err
	}
	if spc.spcClient == nil {
		return "", fmt.Errorf("Something's gone terribly wrong, spc.spcClient is nil")
	}
	// name := appendRandomSuffixTo("autoscaling")
	// name := apiv1.SimpleNameGenerator.GenerateName("autoscaling-")
	name := "autoscaling-" + id
	nodeClass := SpcNodeClass{nodeType}
	newNodeGroup := &SpcNodeGroup{
		id:        name,
		minSize:   minSize,
		maxSize:   maxSize,
		Class:     nodeClass,
		k8sclient: spc.k8sclient,
		manager:   spc.spcManager,
	}
	spc.nodeGroups = append(spc.nodeGroups, newNodeGroup)
	return name, nil
}

func tokenizeNodeGroupSpec(spec string) (minSize, maxSize int, nodeSizeProviderString, id string, err error) {

	tokens := strings.SplitN(spec, ":", 4)
	if len(tokens) != 4 {
		return minSize, maxSize, nodeSizeProviderString, id, fmt.Errorf("bad node configuration: %s", spec)
	}

	if size, err := strconv.Atoi(tokens[0]); err == nil {
		if size < 0 {
			return minSize, maxSize, nodeSizeProviderString, id, fmt.Errorf("min size must be >= 0")
		}
		minSize = size
	} else {
		return minSize, maxSize, nodeSizeProviderString, id, fmt.Errorf("failed to set min size: %s, expected integer", tokens[0])
	}

	if size, err := strconv.Atoi(tokens[1]); err == nil {
		if size < minSize {
			return minSize, maxSize, nodeSizeProviderString, id, fmt.Errorf("max size must be greater or equal to min size")
		}
		maxSize = size
	} else {
		return minSize, maxSize, nodeSizeProviderString, id, fmt.Errorf("failed to set max size: %s, expected integer", tokens[1])
	}

	nodeSizeProviderString = tokens[2] // TODO validate + error checking
	id = tokens[3]

	return minSize, maxSize, nodeSizeProviderString, id, nil
}

// Name returns name of the cloud provider.
func (spc *SpcCloudProvider) Name() string {
	return "spc"
}

// NodeGroups returns all node groups configured for this cloud provider.
func (spc *SpcCloudProvider) NodeGroups() []cloudprovider.NodeGroup {
	var result []cloudprovider.NodeGroup
	for _, nodeGroup := range spc.nodeGroups {
		result = append(result, *nodeGroup)
	}
	return result
}

// NodeGroupForNode returns the node group for the given node
// cloudprovider text says, "nil if the node
// should not be processed by cluster autoscaler, or non-nil error if such
// occurred." but the implementation requires a non-nil error, value of the NodeGroup
// is _not_ consistently checked against nil
func (spc *SpcCloudProvider) NodeGroupForNode(node *apiv1.Node) (cloudprovider.NodeGroup, error) {

	nodeGroupName, error := nodeGroupNameForNode(node)
	if error != nil {
		glog.V(2).Infof("Node not manageable %s, can't get nodeGroupName", node.Spec.ExternalID)
		return nil, nil //&SpcNodeGroup{}, nil //fmt.Errorf("Node not manageable <%s>, can't get nodeGroupName", node.Name)
	}
	if nodeGroupName == "" {
		glog.V(5).Infof("Node not manageable %s, nodeGroupName is empty", node.Spec.ExternalID)
		return nil, nil //&SpcNodeGroup{}, nil //fmt.Errorf("Node not manageable <%s>, nodeGroupName is empty", node.Name)
	}
	for _, nodeGroup := range spc.nodeGroups {
		if nodeGroup.Id() == nodeGroupName {
			glog.V(5).Infof("Matched nodeGroupName %s to node %s", nodeGroupName, node.Spec.ExternalID)
			return nodeGroup, nil
		}
	}

	glog.V(2).Infof("Failed to match nodeGroupName %s of node %s", nodeGroupName, node.Spec.ExternalID)
	return nil, nil //&SpcNodeGroup{}, nil //fmt.Errorf("Failed to matched nodeGroupName %s", nodeGroupName)
}
