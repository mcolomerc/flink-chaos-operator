/*
Copyright 2024 The Flink Chaos Operator Authors.

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

// export_test.go exposes internal symbols for black-box testing from
// package apiserver_test. Only compiled during `go test`.
package apiserver

import v1alpha1 "github.com/flink-chaos-operator/api/v1alpha1"

// BuildNetworkChaosSpec is the exported test alias for buildNetworkChaosSpec.
var BuildNetworkChaosSpec = buildNetworkChaosSpec

// BuildResourceExhaustionSpec is the exported test alias for buildResourceExhaustionSpec.
var BuildResourceExhaustionSpec = buildResourceExhaustionSpec

// StorageType is the exported test alias for storageType.
var StorageType = storageType

// DeduplicateConnections is the exported test alias for deduplicateConnections.
var DeduplicateConnections = deduplicateConnections

// ToChaosRunSummary is the exported test alias for toChaosRunSummary.
func ToChaosRunSummary(run *v1alpha1.ChaosRun) ChaosRunSummary {
	return toChaosRunSummary(run)
}

// ToChaosRunDetail is the exported test alias for toChaosRunDetail.
func ToChaosRunDetail(run *v1alpha1.ChaosRun) ChaosRunDetail {
	return toChaosRunDetail(run)
}
