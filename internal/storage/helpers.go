package storage

func QuickSelectResults(results []*SearchResult, k int) {
	if len(results) <= k {
		return
	}

	left, right := 0, len(results)-1
	for left < right {
		pivotIdx := partitionResults(results, left, right)
		if pivotIdx == k {
			return
		} else if pivotIdx < k {
			left = pivotIdx + 1
		} else {
			right = pivotIdx - 1
		}
	}
}

func partitionResults(results []*SearchResult, left, right int) int {
	pivot := results[right].Score
	i := left
	for j := left; j < right; j++ {
		if results[j].Score > pivot {
			results[i], results[j] = results[j], results[i]
			i++
		}
	}
	results[i], results[right] = results[right], results[i]
	return i
}

func SortResultsByScore(results []*SearchResult) {
	for i := 0; i < len(results)-1; i++ {
		for j := i + 1; j < len(results); j++ {
			if results[i].Score < results[j].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
}
