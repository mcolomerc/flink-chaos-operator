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

package main

import (
	"embed"
	"io/fs"
)

// embeddedUI holds the compiled React frontend placed into dist/ by
// `npm run build`. The dist/ directory is populated as part of the Docker
// multi-stage build (see Dockerfile.ui-server) or by running
// `make build-ui` locally before `go build`.
//
//go:embed all:dist
var embeddedUI embed.FS

// uiFS returns the dist/ sub-tree so that http.FileServerFS serves files
// at the root path rather than under a /dist/ prefix.
func uiFS() fs.FS {
	sub, err := fs.Sub(embeddedUI, "dist")
	if err != nil {
		// dist/ is always present (at minimum the .gitkeep placeholder),
		// so this path should never be reached in practice.
		panic("embedded dist/ sub-tree not found: " + err.Error())
	}
	return sub
}
