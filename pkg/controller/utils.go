/*
Copyright AppsCode Inc. and Contributors.

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

package controller

import (
	"fmt"
	"strconv"
	"strings"

	"gomodules.xyz/sets"
)

func getUpdatedPodLists(existing, podLists []string) []string {
	if len(existing) > len(podLists) {
		return handleDownScaling(existing, podLists)
	}
	return handleUpdateOrUpScaling(existing, podLists)
}

// Both the handleDownScaling & handleUpdateOrUpScaling func is to sort the new 'podLists' according to the sequence of 'existing'.

// scaleDown example: existing=[2, 3, 0, 1], podLists=[1, 3]. output will be = [3, 1]. Because 3 appears before 1 in the 'existing' array.
func handleDownScaling(existing, podLists []string) []string {
	newPods := make([]string, 0)

	idxMap := make(map[string]int)
	for idx, pod := range podLists {
		idxMap[pod] = idx
	}

	for _, pod := range existing {
		idx, exists := idxMap[pod]
		if exists {
			newPods = append(newPods, pod)
			podLists[idx] = ""
		}
	}
	for _, pod := range podLists {
		if pod != "" {
			newPods = append(newPods, pod)
		}
	}
	return newPods
}

// scaleUp example: existing=[1, 3], podLists=[2, 3, 0, 1]. output will be = [1, 3, 2, 0]. Keeping [1,3] as it is. Then appending the remaining ones from 'podLists'.
func handleUpdateOrUpScaling(existing, podLists []string) []string {
	newPods := make([]string, len(podLists))

	idxMap := make(map[string]int)
	for idx, pod := range podLists {
		idxMap[pod] = idx
	}

	for i, pod := range existing {
		idx, exists := idxMap[pod]
		if exists {
			newPods[i] = pod
			podLists[idx] = ""
		}
	}
	nextAvailable := 0
	for _, pod := range podLists {
		if pod == "" {
			continue
		}
		nextAvailable = getNextAvailableIndex(nextAvailable, newPods)
		newPods[nextAvailable] = pod
		nextAvailable++
	}
	return newPods
}

func getNextAvailableIndex(next int, pods []string) int {
	for i := next; i < len(pods); i++ {
		if pods[i] == "" {
			return i
		}
	}
	return 0
}

func isReadable(verbs []string) bool {
	return sets.NewString(verbs...).HasAll("get", "list", "watch")
}

// EvaluateJSONPath evaluates a simple JSONPath expression on an unstructured object
// Supports paths like ".spec.databaseRef.name"
// Returns the value as a string and true if found, empty string and false otherwise
func EvaluateJSONPath(obj map[string]any, jsonPath string) (string, bool) {
	if obj == nil || jsonPath == "" {
		return "", false
	}

	// Remove leading dot if present
	jsonPath = strings.TrimPrefix(jsonPath, ".")

	// Split the path into parts
	parts := strings.Split(jsonPath, ".")

	// Navigate through the object structure
	current := any(obj)
	for i, part := range parts {
		if part == "" {
			continue
		}

		// Try to cast current to map[string]any
		currentMap, ok := current.(map[string]any)
		if !ok {
			return "", false
		}

		// Get the next value
		next, exists := currentMap[part]
		if !exists {
			return "", false
		}

		// If this is the last part, try to convert to string
		if i == len(parts)-1 {
			switch v := next.(type) {
			case string:
				return v, true
			case int:
				return strconv.Itoa(v), true
			case int64:
				return strconv.FormatInt(v, 10), true
			case float64:
				return strconv.FormatFloat(v, 'f', -1, 64), true
			case bool:
				return strconv.FormatBool(v), true
			default:
				return fmt.Sprintf("%v", v), true
			}
		}

		current = next
	}

	return "", false
}

func buildPodList(ctrlName string, replCount int32) []string {
	pods := make([]string, replCount)
	for c := int32(0); c < replCount; c++ {
		pods[c] = fmt.Sprintf("%s-%d", ctrlName, c)
	}
	return pods
}
