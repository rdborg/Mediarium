//go:build race

package parser_test

// slowdown is how much slower the race detector makes the timing tests, so
// their limits still catch a runaway loop without failing on a slow runner.
const slowdown = 25
