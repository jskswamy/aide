package provision

// ReverseLookup performs a linear search over a forward map to find the key
// corresponding to a given value. Returns fallback if the value is not found.
// This is O(n) by design — it scans the map by value for cases where the
// forward map is not inverted at initialization (e.g., runtime-only maps).
// For maps that are statically inverted, prefer direct map lookup over the
// pre-inverted map.
func ReverseLookup[K, V comparable](m map[K]V, v V, fallback K) K {
	for k, mapVal := range m {
		if mapVal == v {
			return k
		}
	}
	return fallback
}
