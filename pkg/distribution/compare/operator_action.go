// Copyright 2024 sealos.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package compare

type OperatorAction string

const (
	OperatorActionCommitOrReapplyLocalOverlay    OperatorAction = "commitOrReapplyLocalOverlay"
	OperatorActionPromoteToLocalPatch            OperatorAction = "promoteToLocalPatch"
	OperatorActionRevertOrUpdateGlobalBaseline   OperatorAction = "revertOrUpdateGlobalBaseline"
	OperatorActionCommitOrReapplyLocalInput      OperatorAction = "commitOrReapplyLocalInput"
	OperatorActionUpdateLocalInputAndRerender    OperatorAction = "updateLocalInputAndRerender"
	OperatorActionRerenderOrUpdateGlobalBaseline OperatorAction = "rerenderOrUpdateGlobalBaseline"
	OperatorActionManualReview                   OperatorAction = "manualReview"
)

type OperatorActionMetadata struct {
	AllowsDirectCommit  bool `json:"allowsDirectCommit,omitempty" yaml:"allowsDirectCommit,omitempty"`
	AllowsDirectRevert  bool `json:"allowsDirectRevert,omitempty" yaml:"allowsDirectRevert,omitempty"`
	RequiresBundleMatch bool `json:"requiresBundleMatch,omitempty" yaml:"requiresBundleMatch,omitempty"`
}

func metadataForOperatorAction(action OperatorAction) *OperatorActionMetadata {
	switch action {
	case OperatorActionCommitOrReapplyLocalOverlay:
		return &OperatorActionMetadata{
			AllowsDirectCommit:  true,
			AllowsDirectRevert:  true,
			RequiresBundleMatch: true,
		}
	case OperatorActionRevertOrUpdateGlobalBaseline:
		return &OperatorActionMetadata{
			AllowsDirectRevert:  true,
			RequiresBundleMatch: true,
		}
	case OperatorActionCommitOrReapplyLocalInput:
		return &OperatorActionMetadata{
			AllowsDirectCommit:  true,
			AllowsDirectRevert:  true,
			RequiresBundleMatch: true,
		}
	case OperatorActionUpdateLocalInputAndRerender,
		OperatorActionPromoteToLocalPatch,
		OperatorActionRerenderOrUpdateGlobalBaseline,
		OperatorActionManualReview:
		return &OperatorActionMetadata{}
	default:
		return nil
	}
}
