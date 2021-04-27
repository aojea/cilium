/*
Copyright 2021 Google LLC

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

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "gke-internal/gke-node-firewall/pkg/apis/nodenetworkpolicy/v1alpha1"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// NodeNetworkPolicyLister helps list NodeNetworkPolicies.
// All objects returned here must be treated as read-only.
type NodeNetworkPolicyLister interface {
	// List lists all NodeNetworkPolicies in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.NodeNetworkPolicy, err error)
	// Get retrieves the NodeNetworkPolicy from the index for a given name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.NodeNetworkPolicy, error)
	NodeNetworkPolicyListerExpansion
}

// nodeNetworkPolicyLister implements the NodeNetworkPolicyLister interface.
type nodeNetworkPolicyLister struct {
	indexer cache.Indexer
}

// NewNodeNetworkPolicyLister returns a new NodeNetworkPolicyLister.
func NewNodeNetworkPolicyLister(indexer cache.Indexer) NodeNetworkPolicyLister {
	return &nodeNetworkPolicyLister{indexer: indexer}
}

// List lists all NodeNetworkPolicies in the indexer.
func (s *nodeNetworkPolicyLister) List(selector labels.Selector) (ret []*v1alpha1.NodeNetworkPolicy, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.NodeNetworkPolicy))
	})
	return ret, err
}

// Get retrieves the NodeNetworkPolicy from the index for a given name.
func (s *nodeNetworkPolicyLister) Get(name string) (*v1alpha1.NodeNetworkPolicy, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("nodenetworkpolicy"), name)
	}
	return obj.(*v1alpha1.NodeNetworkPolicy), nil
}
