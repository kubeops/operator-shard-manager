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
	"math"
	"strconv"

	"github.com/cespare/xxhash/v2"
	"gomodules.xyz/consistent"
)

// consistent package doesn't provide a default hashing function.
// You should provide a proper one to distribute keys/members uniformly.
type hasher struct{}

func (h hasher) Sum64(data []byte) uint64 {
	// you should use a proper hash function for uniformity.
	return xxhash.Sum64(data)
}

type Member struct{ ID int }

func (M Member) String() string {
	return strconv.Itoa(M.ID)
}

var _ consistent.Member = Member{}

var PrimeNumbers = []int{
	2, 3, 5, 7, 11, 13, 17, 19, 23, 29,
	31, 37, 41, 43, 47, 53, 59, 61, 67, 71,
	73, 79, 83, 89, 97, 101, 103, 107, 109, 113,
	127, 131, 137, 139, 149, 151, 157, 163, 167, 173,
	179, 181, 191, 193, 197, 199, 211, 223, 227, 229,
	233, 239, 241, 251, 257, 263, 269, 271, 277, 281,
	283, 293, 307, 311, 313, 317, 331, 337, 347, 349,
	353, 359, 367, 373, 379, 383, 389, 397, 401, 409,
	419, 421, 431, 433, 439, 443, 449, 457, 461, 463,
	467, 479, 487, 491, 499,
}

func getBetterPartitionCount(members int, load float64) int {
	ratio := -1.0
	candidate := members
	for _, prime := range PrimeNumbers {
		if prime <= members {
			continue
		}
		avgLoad := (float64(prime) / float64(members)) * load
		iavgLoad := int(math.Ceil(avgLoad))
		mratio := float64(prime%iavgLoad) / float64(iavgLoad)
		if mratio > ratio {
			ratio = mratio
			candidate = prime
		}
	}
	return candidate
}

func newConsistentConfig(members []consistent.Member, shardCount int) *consistent.Consistent {
	return consistent.New(members, consistent.Config{
		PartitionCount:    getBetterPartitionCount(shardCount, 1.0),
		ReplicationFactor: 1,
		Load:              1.0,
		Hasher:            hasher{},
	})
}
