package queue

// blockBitmap stores dynamic blocking bits for a queue entry.
type blockBitmap struct {
	words []uint64
}

func (b *blockBitmap) ensure(word int) {
	if word < 0 {
		return
	}
	if len(b.words) > word {
		return
	}
	needed := word + 1
	growth := needed - len(b.words)
	if growth <= 0 {
		return
	}
	b.words = append(b.words, make([]uint64, growth)...)
}

func (b *blockBitmap) set(index BlockIndex) bool {
	word := int(index) / 64
	bit := uint(index) % 64
	b.ensure(word)
	old := b.words[word]
	mask := uint64(1) << bit
	b.words[word] |= mask
	return old&mask == 0
}

func (b *blockBitmap) clear(index BlockIndex) bool {
	word := int(index) / 64
	bit := uint(index) % 64
	if word < 0 || word >= len(b.words) {
		return false
	}
	old := b.words[word]
	mask := uint64(1) << bit
	b.words[word] &^= mask
	return old&mask != 0
}

func (b *blockBitmap) isZero() bool {
	for _, w := range b.words {
		if w != 0 {
			return false
		}
	}
	return true
}

func (b *blockBitmap) reset() {
	for i := range b.words {
		b.words[i] = 0
	}
}


