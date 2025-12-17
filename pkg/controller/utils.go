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
	"k8s.io/apimachinery/pkg/runtime/schema"
)

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

func isReadable(verbs []string) bool {
	return sets.NewString(verbs...).HasAll("get", "list", "watch")
}

// EvaluateJSONPath evaluates a simple JSONPath expression on an unstructured object
// Supports paths like ".spec.databaseRef.name" or "spec.databaseRef.name"
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

func GetDatabaseGVKFromOpsRequestGVK(gvk schema.GroupVersionKind) schema.GroupVersionKind {
	switch gvk.Kind {
	// v1 databases
	case ResourceKindElasticsearchOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1",
			Kind:    "Elasticsearch",
		}
	case ResourceKindKafkaOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1",
			Kind:    "Kafka",
		}
	case ResourceKindMariadbOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1",
			Kind:    "MariaDB",
		}
	case ResourceKindMemcachedOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1",
			Kind:    "Memcached",
		}
	case ResourceKindMongodbOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1",
			Kind:    "MongoDB",
		}
	case ResourceKindMysqlOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1",
			Kind:    "MySQL",
		}
	case ResourceKindPerconaxtradbOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1",
			Kind:    "PerconaXtraDB",
		}
	case ResourceKindPgbouncerOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1",
			Kind:    "PgBouncer",
		}
	case ResourceKindPostgresOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1",
			Kind:    "Postgres",
		}
	case ResourceKindProxysqlOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1",
			Kind:    "ProxySQL",
		}
	case ResourceKindRedisOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1",
			Kind:    "Redis",
		}
	case ResourceKindRedisSentinelOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1",
			Kind:    "RedisSentinel",
		}

	// v1alpha2 databases
	case ResourceKindCassandraOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1alpha2",
			Kind:    "Cassandra",
		}
	case ResourceKindClickhouseOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1alpha2",
			Kind:    "Clickhouse",
		}
	case ResourceKindDruidOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1alpha2",
			Kind:    "Druid",
		}
	case ResourceKindFerretdbOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1alpha2",
			Kind:    "FerretDB",
		}
	case ResourceKindHazelcastOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1alpha2",
			Kind:    "Hazelcast",
		}
	case ResourceKindIgniteOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1alpha2",
			Kind:    "Ignite",
		}
	case ResourceKindMssqlserverOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1alpha2",
			Kind:    "MSSQLServer",
		}
	case ResourceKindPgpoolOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1alpha2",
			Kind:    "Pgpool",
		}
	case ResourceKindRabbitmqOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1alpha2",
			Kind:    "RabbitMQ",
		}
	case ResourceKindSinglestoreOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1alpha2",
			Kind:    "Singlestore",
		}
	case ResourceKindSolrOpsRequest:
		return schema.GroupVersionKind{
			Group:   "kubedb.com",
			Version: "v1alpha2",
			Kind:    "Solr",
		}
	}
	return schema.GroupVersionKind{}
}

func revampPodListsForStatefulset(pods []string, sts string) []string {
	maxId := -1
	for _, pod := range pods {
		id, err := strconv.Atoi(getPodOrdinal(pod))
		if err != nil {
			continue
		}
		maxId = max(maxId, id)
	}
	if maxId == -1 {
		return pods
	}
	newPods := make([]string, 0)
	for i := 0; i <= maxId; i++ {
		newPods = append(newPods, fmt.Sprintf("%s-%d", sts, i))
	}
	return newPods
}

func getPodOrdinal(pod string) string {
	return strings.Split(pod, "-")[len(strings.Split(pod, "-"))-1]
}
