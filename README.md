# operator-shard-manager

Operator Shard Manager applies a label `shard.operator.k8s.appscode.com/<shard-config-name>: <shard-index>` on Kubernetes resources and assigns indices to controllers that manage those resources.

This uses [consistent hashing with bounded load](https://research.google/blog/consistent-hashing-with-bounded-loads/) to achieve both uniformity and consistency across operator pods managing resources.

## Key Considerations

- A sharding aware operator implementation can use label to only watch and manage resources assigned to its own shard. But one has to be careful not consider a missing object from controller cache as deletion, as the object might has been moved to a different shard to balance sharding when operator pod count changes.

- An alternative option will be to watch all objects of a given resource kind and use predicates to skip managing resources that are not in assigned to an operator pods's shard index. This will not reduce the list call response size. This approach has the benefit of accessing refrenced resources of the same kind that are not in the same shard.

## Samples

You can find example ShardConfiguration yamls [here](hack/samples).
