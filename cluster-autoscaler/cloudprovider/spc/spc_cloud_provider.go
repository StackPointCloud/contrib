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
	"k8s.io/contrib/cluster-autoscaler/cloudprovider"
	kube_api "k8s.io/kubernetes/pkg/api"
)

// SpcNodeGroup implements NodeGroup
type SpcNodeGroup struct {
	id      string
	maxSize int
	minSize int
	manager *NodeManager
}

// MaxSize returns maximum size of the node group.
func (sng *SpcNodeGroup) MaxSize() int {
	return sng.maxSize
}

// MinSize returns minimum size of the node group.
func (sng *SpcNodeGroup) MinSize() int {
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
func (sng *SpcNodeGroup) TargetSize() (int, error) {

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
func (sng *SpcNodeGroup) IncreaseSize(delta int) error {
	_, err := sng.manager.IncreaseSize(delta, sng.Id())
	return err
}

// DeleteNodes deletes nodes from this node group. Error is returned either on
// failure or if the given node doesn't belong to this node group. This function
// should wait until node group size is updated.
func (sng *SpcNodeGroup) DeleteNodes(nodes []*kube_api.Node) error {
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

func labelValueForNode(key string, node *kube_api.Node) (string, error) {
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

func instanceIDForNode(node *kube_api.Node) (string, error) {
	return labelValueForNode("stackpoint.io/instance_id", node)
}

func nodeGroupNameForNode(node *kube_api.Node) (string, error) {
	return labelValueForNode("stackpoint.io/node_group", node)
}

// Id returns an unique identifier of the node group.
func (sng *SpcNodeGroup) Id() string {
	return sng.id
}

// Debug returns a string containing all information regarding this node group.
func (sng *SpcNodeGroup) Debug() string {
	var msg string
	target, err := sng.TargetSize()
	if err != nil {
		msg = err.Error()
	}
	return fmt.Sprintf("%s (%d:%d) (%d) %s", sng.Id(), sng.MinSize(), sng.MaxSize(), target, msg)
}

// SpcCloudProvider implements CloudProvider
type SpcCloudProvider struct {
	spcClient  *ClusterClient
	nodeGroups []*SpcNodeGroup
}

// BuildSpcCloudProvider builds CloudProvider implementation for stackpointio.
func BuildSpcCloudProvider(spcClient *ClusterClient, specs []string) (*SpcCloudProvider, error) {
	if spcClient == nil {
		return nil, fmt.Errorf("ClusterClient is nil")
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("No node group is specified")
	} else if len(specs) > 1 {
		return nil, fmt.Errorf("Multiple node groups not supported at this time")
	}
	spc := &SpcCloudProvider{
		spcClient:  spcClient,
		nodeGroups: make([]*SpcNodeGroup, 0),
	}
	if err := spc.addNodeGroup(specs[0], "autoscaling"); err != nil {
		return nil, err
	}
	return spc, nil
}

// addNodeGroup adds node group defined in string spec. Format:
// minNodes:maxNodes:typeofNode
func (spc *SpcCloudProvider) addNodeGroup(spec, name string) error {
	glog.V(5).Infof("Node group specification: %s", spec)
	minSize, maxSize, nodeType, err := tokenizeNodeGroupSpec(spec)
	if err != nil {
		return err
	}
	if spc.spcClient == nil {
		return fmt.Errorf("Something's gone terribly wrong, spc.spcClient is nil")
	}
	manager := CreateNodeManager(nodeType, spc.spcClient)
	err = manager.Update()
	if err != nil {
		return err
	}
	newNodeGroup := &SpcNodeGroup{
		id:      name,
		minSize: minSize,
		maxSize: maxSize,
		manager: manager,
	}
	spc.nodeGroups = append(spc.nodeGroups, newNodeGroup)
	return nil
}

func tokenizeNodeGroupSpec(spec string) (minSize, maxSize int, nodeSizeProviderString string, err error) {

	tokens := strings.SplitN(spec, ":", 3)
	if len(tokens) != 3 {
		return minSize, maxSize, nodeSizeProviderString, fmt.Errorf("bad node configuration: %s", spec)
	}

	if size, err := strconv.Atoi(tokens[0]); err == nil {
		if size < 0 {
			return minSize, maxSize, nodeSizeProviderString, fmt.Errorf("min size must be >= 0")
		}
		minSize = size
	} else {
		return minSize, maxSize, nodeSizeProviderString, fmt.Errorf("failed to set min size: %s, expected integer", tokens[0])
	}

	if size, err := strconv.Atoi(tokens[1]); err == nil {
		if size < minSize {
			return minSize, maxSize, nodeSizeProviderString, fmt.Errorf("max size must be greater or equal to min size")
		}
		maxSize = size
	} else {
		return minSize, maxSize, nodeSizeProviderString, fmt.Errorf("failed to set max size: %s, expected integer", tokens[1])
	}

	nodeSizeProviderString = tokens[2] // TODO validate + error checking

	return minSize, maxSize, nodeSizeProviderString, nil
}

// Name returns name of the cloud provider.
func (spc *SpcCloudProvider) Name() string {
	return "spc"
}

// NodeGroups returns all node groups configured for this cloud provider.
func (spc *SpcCloudProvider) NodeGroups() []cloudprovider.NodeGroup {
	result := make([]cloudprovider.NodeGroup, 0, len(spc.nodeGroups))
	for _, nodeGroups := range spc.nodeGroups {
		result = append(result, nodeGroups)
	}
	return result
}

// NodeGroupForNode returns the node group for the given node, nil if the node
// should not be processed by cluster autoscaler, or non-nil error if such
// occurred.
func (spc *SpcCloudProvider) NodeGroupForNode(node *kube_api.Node) (cloudprovider.NodeGroup, error) {

	nodeGroupName, error := nodeGroupNameForNode(node)
	if error != nil {
		glog.V(2).Infof("Error node not manageable %s, can't get nodeGroupName", node.Spec.ExternalID)
		return nil, nil
	}
	if nodeGroupName == "" {
		glog.V(5).Infof("Node not manageable %s, nodeGroupName is empty", node.Spec.ExternalID)
		return nil, nil
	}
	for _, nodeGroup := range spc.nodeGroups {
		if nodeGroup.Id() == nodeGroupName {
			glog.V(5).Infof("Matched nodeGroupName %s", nodeGroupName)
			return nodeGroup, nil
		}
	}
	glog.V(2).Infof("Failed to matched nodeGroupName %s", nodeGroupName)

	return nil, nil
}
