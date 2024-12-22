package storage

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"
)

// Correct (but memory inefficient) implementation of setMinus.
func setMinusCorrect(a, b []string) []string {
	c := map[string]struct{}{}
	for _, x := range a {
		c[x] = struct{}{}
	}
	for _, x := range b {
		delete(c, x)
	}
	result := make([]string, 0, len(c))
	for x := range c {
		result = append(result, x)
	}
	return result
}

func generateRandomCases(seed int64) [][2]string {
	rand.Seed(seed)
	cases := make([][2]string, 1000)
	for i := range cases {
		a := make([]byte, 26)
		b := make([]byte, 26)
		for j := range a {
			a[j] = byte('a' + j)
			b[j] = byte('a' + j)
		}
		rand.Shuffle(len(a), func(i, j int) { a[i], a[j] = a[j], a[i] })
		rand.Shuffle(len(b), func(i, j int) { b[i], b[j] = b[j], b[i] })
		cases[i] = [2]string{string(a), string(b)}
	}
	return cases
}

func TestSetMinus(t *testing.T) {
	casesManual := [][2]string{
		{"1", "1"},
		{"1", "2"},
		{"123", "23"},
		{"123", "13"},
		{"123", "12"},
		{"12", "123"},
		{"13", "123"},
		{"3", "123"},
		{"2", "123"},
		{"1", "123"},
	}
	casesRandom := generateRandomCases(0)
	cases := append(casesRandom, casesManual...)

	for i, caseData := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			a := make([]string, 0, len(caseData[0]))
			for _, c := range caseData[0] {
				a = append(a, string(c))
			}
			b := make([]string, 0, len(caseData[1]))
			for _, c := range caseData[1] {
				b = append(b, string(c))
			}
			got := setMinus(a, b)
			expected := setMinusCorrect(a, b)
			if !slices.Equal(got, expected) {
				t.Fatalf("%s\\%s: expected %v, got %v", caseData[0], caseData[1], expected, got)
			}
		})
	}
}
