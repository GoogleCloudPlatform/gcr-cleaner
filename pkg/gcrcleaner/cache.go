package gcrcleaner

// Cache is an interface used by the PubSub() function to prevent duplicate
// messages from being processed.
type Cache interface {
	// Insert inserts the item into the cache. If the item already exists, this
	// method returns true.
	Insert(string) bool

	// Stop stops the cache. When Stop returns, the cache must not perform any
	// additionally processing.
	Stop()
}
