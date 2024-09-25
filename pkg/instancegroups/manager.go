/*
Copyright 2015 The Kubernetes Authors.

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

package instancegroups

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"google.golang.org/api/compute/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/ingress-gce/pkg/events"
	"k8s.io/ingress-gce/pkg/utils/namer"
	"k8s.io/ingress-gce/pkg/utils/zonegetter"
	"k8s.io/klog/v2"

	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/ingress-gce/pkg/utils"
)

const (
	// State string required by gce library to list all instances.
	allInstances = "ALL"
)

// manager implements Manager.
type manager struct {
	cloud              Provider
	ZoneGetter         *zonegetter.ZoneGetter
	namer              namer.BackendNamer
	recorder           record.EventRecorder
	instanceLinkFormat string
	maxIGSize          int
}

type recorderSource interface {
	Recorder(ns string) record.EventRecorder
}

// ManagerConfig is used for Manager constructor.
type ManagerConfig struct {
	// Cloud implements Provider, used to sync Kubernetes nodes with members of the cloud InstanceGroup.
	Cloud      Provider
	Namer      namer.BackendNamer
	Recorders  recorderSource
	BasePath   string
	ZoneGetter *zonegetter.ZoneGetter
	MaxIGSize  int
}

// NewManager creates a new node pool using ManagerConfig.
func NewManager(config *ManagerConfig) Manager {
	return &manager{
		cloud:              config.Cloud,
		namer:              config.Namer,
		recorder:           config.Recorders.Recorder(""), // No namespace
		instanceLinkFormat: config.BasePath + "zones/%s/instances/%s",
		ZoneGetter:         config.ZoneGetter,
		maxIGSize:          config.MaxIGSize,
	}
}

// EnsureInstanceGroupsAndPorts creates or gets an instance group if it doesn't exist
// and adds the given ports to it. Returns a list of one instance group per zone,
// all of which have the exact same named ports.
func (m *manager) EnsureInstanceGroupsAndPorts(name string, ports []int64) (igs []*compute.InstanceGroup, err error) {
	// Instance groups need to be created only in zones that have ready nodes.
	zones, err := m.ZoneGetter.List(zonegetter.CandidateNodesFilter, klog.TODO())
	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		ig, err := m.ensureInstanceGroupAndPorts(name, zone, ports)
		if err != nil {
			return nil, err
		}

		igs = append(igs, ig)
	}
	return igs, nil
}

func (m *manager) ensureInstanceGroupAndPorts(name, zone string, ports []int64) (*compute.InstanceGroup, error) {
	klog.V(3).Infof("Ensuring instance group %s in zone %s. Ports: %v", name, zone, ports)

	ig, err := m.Get(name, zone)
	if err != nil && !utils.IsHTTPErrorCode(err, http.StatusNotFound) {
		klog.Errorf("Failed to get instance group %v/%v, err: %v", zone, name, err)
		return nil, err
	}

	if ig == nil {
		klog.V(3).Infof("Creating instance group %v/%v.", zone, name)
		if err = m.cloud.CreateInstanceGroup(&compute.InstanceGroup{Name: name}, zone); err != nil {
			// Error may come back with StatusConflict meaning the instance group was created by another controller
			// possibly the Service Manager for internal load balancers.
			if utils.IsHTTPErrorCode(err, http.StatusConflict) {
				klog.Warningf("Failed to create instance group %v/%v due to conflict status, but continuing sync. err: %v", zone, name, err)
			} else {
				klog.Errorf("Failed to create instance group %v/%v, err: %v", zone, name, err)
				return nil, err
			}
		}
		ig, err = m.Get(name, zone)
		if err != nil {
			klog.Errorf("Failed to get instance group %v/%v after ensuring existence, err: %v", zone, name, err)
			return nil, err
		}
	} else {
		klog.V(2).Infof("Instance group %v/%v already exists.", zone, name)
	}

	// Build map of existing ports
	existingPorts := map[int64]bool{}
	for _, np := range ig.NamedPorts {
		existingPorts[np.Port] = true
	}

	// Determine which ports need to be added
	var newPorts []int64
	for _, p := range ports {
		if existingPorts[p] {
			klog.V(5).Infof("Instance group %v/%v already has named port %v", zone, ig.Name, p)
			continue
		}
		newPorts = append(newPorts, p)
	}

	// Build slice of NamedPorts for adding
	var newNamedPorts []*compute.NamedPort
	for _, port := range newPorts {
		newNamedPorts = append(newNamedPorts, &compute.NamedPort{Name: m.namer.NamedPort(port), Port: port})
	}

	if len(newNamedPorts) > 0 {
		klog.V(3).Infof("Instance group %v/%v does not have ports %+v, adding them now.", zone, name, newPorts)
		if err := m.cloud.SetNamedPortsOfInstanceGroup(ig.Name, zone, append(ig.NamedPorts, newNamedPorts...)); err != nil {
			return nil, err
		}
	}

	return ig, nil
}

// DeleteInstanceGroup deletes the given IG by name, from all zones.
func (m *manager) DeleteInstanceGroup(name string) error {
	var errs []error

	zones, err := m.ZoneGetter.List(zonegetter.AllNodesFilter, klog.TODO())
	if err != nil {
		return err
	}
	for _, zone := range zones {
		if err := m.cloud.DeleteInstanceGroup(name, zone); err != nil {
			if utils.IsNotFoundError(err) {
				klog.V(3).Infof("Instance group %v in zone %v did not exist", name, zone)
			} else if utils.IsInUsedByError(err) {
				klog.V(3).Infof("Could not delete instance group %v in zone %v because it's still in use. Ignoring: %v", name, zone, err)
			} else {
				errs = append(errs, err)
			}
		} else {
			klog.V(3).Infof("Deleted instance group %v in zone %v", name, zone)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%v", errs)
}

// Get returns the Instance Group by name.
func (m *manager) Get(name, zone string) (*compute.InstanceGroup, error) {
	ig, err := m.cloud.GetInstanceGroup(name, zone)
	if err != nil {
		return nil, err
	}

	return ig, nil
}

// List lists the names of all Instance Groups belonging to this cluster.
// It will return only the names of the groups without the zone. If a group
// with the same name exists in more than one zone it will be returned only once.
func (m *manager) List() ([]string, error) {
	var igs []*compute.InstanceGroup

	zones, err := m.ZoneGetter.List(zonegetter.AllNodesFilter, klog.TODO())
	if err != nil {
		return nil, err
	}

	for _, zone := range zones {
		igsForZone, err := m.cloud.ListInstanceGroups(zone)
		if err != nil {
			return nil, err
		}

		for _, ig := range igsForZone {
			igs = append(igs, ig)
		}
	}

	names := sets.New[string]()

	for _, ig := range igs {
		if m.namer.NameBelongsToCluster(ig.Name) {
			names.Insert(ig.Name)
		}
	}
	return names.UnsortedList(), nil
}

// splitNodesByZones takes a list of node names and returns a map of zone:node names.
// It figures out the zones by asking the zoneLister.
func (m *manager) splitNodesByZone(names []string) map[string][]string {
	nodesByZone := map[string][]string{}
	for _, name := range names {
		zone, err := m.ZoneGetter.ZoneForNode(name, klog.TODO())
		if err != nil {
			klog.Errorf("Failed to get zones for %v: %v, skipping", name, err)
			continue
		}
		if _, ok := nodesByZone[zone]; !ok {
			nodesByZone[zone] = []string{}
		}
		nodesByZone[zone] = append(nodesByZone[zone], name)
	}
	return nodesByZone
}

// getInstanceReferences creates and returns the instance references by generating the
// expected instance URLs
func (m *manager) getInstanceReferences(zone string, nodeNames []string) (refs []*compute.InstanceReference) {
	for _, nodeName := range nodeNames {
		refs = append(refs, &compute.InstanceReference{Instance: fmt.Sprintf(m.instanceLinkFormat, zone, canonicalizeInstanceName(nodeName))})
	}
	return refs
}

// Add adds the given instances to the appropriately zoned Instance Group.
func (m *manager) add(groupName string, nodeNames []string, zone string) error {
	events.GlobalEventf(m.recorder, core.EventTypeNormal, events.AddNodes, "Adding %s to InstanceGroup %q", events.TruncatedStringList(nodeNames), groupName)
	klog.Infof("Adding nodes to instance group in zone", "nodeCount", len(nodeNames), "name", groupName, "zone", zone)
	err := m.cloud.AddInstancesToInstanceGroup(groupName, zone, m.getInstanceReferences(zone, nodeNames))
	if err != nil && !utils.IsMemberAlreadyExistsError(err) {
		events.GlobalEventf(m.recorder, core.EventTypeWarning, events.AddNodes, "Error adding %s to InstanceGroup %q: %v", events.TruncatedStringList(nodeNames), groupName, err)
		return err
	}
	return nil
}

// Remove removes the given instances from the appropriately zoned Instance Group.
func (m *manager) remove(groupName string, nodeNames []string, zone string) error {
	events.GlobalEventf(m.recorder, core.EventTypeNormal, events.RemoveNodes, "Removing %s from InstanceGroup %q", events.TruncatedStringList(nodeNames), groupName)

	klog.Infof("Removing nodes from instance group in zone", "nodeCount", len(nodeNames), "name", groupName, "zone", zone)
	if err := m.cloud.RemoveInstancesFromInstanceGroup(groupName, zone, m.getInstanceReferences(zone, nodeNames)); err != nil {
		events.GlobalEventf(m.recorder, core.EventTypeWarning, events.RemoveNodes, "Error removing nodes %s from InstanceGroup %q: %v", events.TruncatedStringList(nodeNames), groupName, err)
		return err
	}
	return nil
}

// Sync nodes with the instances in the instance group.
func (m *manager) Sync(nodes []string) (err error) {
	klog.Infof("Syncing nodes", "nodes", events.TruncatedStringList(nodes))

	defer func() {
		// The node pool is only responsible for syncing nodes to instance
		// groups. It never creates/deletes, so if an instance groups is
		// not found there's nothing it can do about it anyway. Most cases
		// this will happen because the backend pool has deleted the instance
		// group, however if it happens because a user deletes the IG by mistake
		// we should just wait till the backend pool fixes it.
		if utils.IsHTTPErrorCode(err, http.StatusNotFound) {
			klog.Infof("Node pool encountered a 404, ignoring: %v", err)
			err = nil
		}
	}()

	// For each zone add up to #m.maxIGSize number of nodes to the instance group
	// If there is more then truncate last nodes (in alphabetical order)
	// the logic should be consistent with cloud-provider-gcp's Legacy L4 ILB Controller:
	// https://github.com/kubernetes/cloud-provider-gcp/blob/fca628cb3bf9267def0abb509eaae87d2d4040f3/providers/gce/gce_loadbalancer_internal.go#L606C1-L675C1
	// the m.maxIGSize should be set to 1000 as is in the cloud-provider-gcp.
	zonedNodes := m.splitNodesByZone(nodes)
	for zone, kubeNodesFromZone := range zonedNodes {
		igName := m.namer.InstanceGroup()
		if len(kubeNodesFromZone) > m.maxIGSize {
			sortedKubeNodesFromZone := sets.NewString(kubeNodesFromZone...).List()
			loggableNodeList := events.TruncatedStringList(sortedKubeNodesFromZone[m.maxIGSize:])
			klog.Infof(fmt.Sprintf("Total number of kubeNodes: %d, truncating to maximum Instance Group size = %d. zone: %s. First truncated instances: %v", len(kubeNodesFromZone), m.maxIGSize, zone, loggableNodeList))
			kubeNodesFromZone = sortedKubeNodesFromZone[:m.maxIGSize]
		}

		kubeNodes := sets.NewString(kubeNodesFromZone...)

		gceNodes := sets.NewString()
		instances, err := m.cloud.ListInstancesInInstanceGroup(igName, zone, allInstances)
		if err != nil {
			klog.Errorf("Failed to list instance from instance group", "zone", zone, "igName", igName)
			return err
		}
		for _, ins := range instances {
			instance, err := utils.KeyName(ins.Instance)
			if err != nil {
				klog.Errorf("Failed to read instance name from ULR, skipping single instance", "Instance URL", ins.Instance)
			}
			gceNodes.Insert(instance)
		}

		removeNodes := gceNodes.Difference(kubeNodes).List()
		addNodes := kubeNodes.Difference(gceNodes).List()

		klog.Infof("Removing nodes", "removeNodes", events.TruncatedStringList(removeNodes))
		klog.Infof("Adding nodes", "addNodes", events.TruncatedStringList(removeNodes))

		start := time.Now()
		if len(removeNodes) != 0 {
			err = m.remove(igName, removeNodes, zone)
			klog.Infof("Remove finished", "name", igName, "err", err, "timeTaken", time.Now().Sub(start), "removeNodes", events.TruncatedStringList(removeNodes))
			if err != nil {
				return err
			}
		}

		start = time.Now()
		if len(addNodes) != 0 {
			err = m.add(igName, addNodes, zone)
			klog.Infof("Add finished", "name", igName, "err", err, "timeTaken", time.Now().Sub(start), "addNodes", events.TruncatedStringList(addNodes))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// canonicalizeInstanceName take a GCE instance 'hostname' and break it down
// to something that can be fed to the GCE API client library.  Basically
// this means reducing 'kubernetes-node-2.c.my-proj.internal' to
// 'kubernetes-node-2' if necessary.
// Helper function is copied from legacy-cloud-provider gce_utils.go
func canonicalizeInstanceName(name string) string {
	ix := strings.Index(name, ".")
	if ix != -1 {
		name = name[:ix]
	}
	return name
}
