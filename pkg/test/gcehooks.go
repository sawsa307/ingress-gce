/*
Copyright 2021 The Kubernetes Authors.

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

package test

import (
	"context"
	"fmt"

	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/filter"
	"github.com/GoogleCloudPlatform/k8s-cloud-provider/pkg/cloud/meta"
	"google.golang.org/api/compute/v1"
)

const (
	FwIPAddress = "10.0.0.1"
)

func ListErrorHook(ctx context.Context, zone string, fl *filter.F, m *cloud.MockInstanceGroups) (bool, []*compute.InstanceGroup, error) {
	return true, nil, fmt.Errorf("ListErrorHook")
}
func ListInstancesWithErrorHook(context.Context, *meta.Key, *compute.InstanceGroupsListInstancesRequest, *filter.F, *cloud.MockInstanceGroups) ([]*compute.InstanceWithNamedPorts, error) {
	return nil, fmt.Errorf("ListInstancesWithErrorHook")
}

func AddInstancesErrorHook(context.Context, *meta.Key, *compute.InstanceGroupsAddInstancesRequest, *cloud.MockInstanceGroups) error {
	return fmt.Errorf("AddInstancesErrorHook")
}

func GetErrorInstanceGroupHook(ctx context.Context, key *meta.Key, m *cloud.MockInstanceGroups) (bool, *compute.InstanceGroup, error) {
	return true, nil, fmt.Errorf("GetErrorInstanceGroupHook")
}

func InsertErrorHook(ctx context.Context, key *meta.Key, obj *compute.InstanceGroup, m *cloud.MockInstanceGroups) (bool, error) {
	return true, fmt.Errorf("InsertErrorHook")
}

func SetNamedPortsErrorHook(context.Context, *meta.Key, *compute.InstanceGroupsSetNamedPortsRequest, *cloud.MockInstanceGroups) error {
	return fmt.Errorf("SetNamedPortsErrorHook")
}

func InsertForwardingRuleHook(ctx context.Context, key *meta.Key, obj *compute.ForwardingRule, m *cloud.MockForwardingRules) (b bool, e error) {
	if obj.IPAddress == "" {
		obj.IPAddress = FwIPAddress
	}
	return false, nil
}

func DeleteForwardingRulesErrorHook(ctx context.Context, key *meta.Key, m *cloud.MockForwardingRules) (bool, error) {
	return true, fmt.Errorf("DeleteForwardingRulesErrorHook")
}

func DeleteAddressErrorHook(ctx context.Context, key *meta.Key, m *cloud.MockAddresses) (bool, error) {
	return true, fmt.Errorf("DeleteAddressErrorHook")
}

func DeleteFirewallsErrorHook(ctx context.Context, key *meta.Key, m *cloud.MockFirewalls) (bool, error) {
	return true, fmt.Errorf("DeleteFirewallsErrorHook")
}

func DeleteBackendServicesErrorHook(ctx context.Context, key *meta.Key, m *cloud.MockRegionBackendServices) (bool, error) {
	return true, fmt.Errorf("DeleteBackendServicesErrorHook")
}

func DeleteHealthCheckErrorHook(ctx context.Context, key *meta.Key, m *cloud.MockRegionHealthChecks) (bool, error) {
	return true, fmt.Errorf("DeleteHealthCheckErrorHook")
}
