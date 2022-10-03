/*
Copyright 2022 Google LLC

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

package v1alpha1

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DataplaneV2Encryption specifies if encryption should be enabled for
// pod to pod network traffic.
type DataplaneV2Encryption struct {
	v1.TypeMeta   `json:",inline"`
	v1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the desired configuration for dataplane-v2 encryption.
	Spec DataplaneV2EncryptionSpec `json:"spec"`
}

// +k8s:openapi-gen=true
type DataplaneV2EncryptionType string

const (
	WireguardDataplaneV2EncryptionType = DataplaneV2EncryptionType("Wireguard")
)

// DataplaneV2EncryptionSpec provides specification for Dataplanev2 Encryption.
// It is one custom resource per cluster.
type DataplaneV2EncryptionSpec struct {
	// Specifies what type of encryption to use.
	// Currently only wireguard is supported.
	Type DataplaneV2EncryptionType `json:"type"`

	// Specifies if encryption should be enabled.
	Enabled bool `json:"enabled"`
}
