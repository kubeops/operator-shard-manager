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

func getUpdatedPodLists(existing, podLists []string) []string {
	if len(existing) > len(podLists) {
		return handleDownScaling(existing, podLists)
	}
	return handleUpdateOrUpScaling(existing, podLists)
}

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

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}
