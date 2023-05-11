package gcrcleaner

type MultiFilter struct {
	filters []TagFilter
}

func BuildMultiFilter(filters ...TagFilter) TagFilter {
	return &MultiFilter{
		filters: filters,
	}
}

func (m *MultiFilter) Name() string {
	return "" // TODO
}

func (m *MultiFilter) Matches(tags []string) bool {
	for _, filter := range m.filters {
		if !filter.Matches(tags) {
			return false
		}
	}
	return true
}
