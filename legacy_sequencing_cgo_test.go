//go:build cgo

package robotgo

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// These adapters never touch the desktop. Legacy composites must keep their
// partial-failure ordering even when later strict composites become fail-fast.
func TestLegacyMoveClickContinuesAfterMoveFailure(t *testing.T) {
	var events []string
	args := []interface{}{"right", true}
	moveClickWith(12, 34, args, func(x, y int, options ...int) error {
		if x != 12 || y != 34 || len(options) != 0 {
			t.Fatal("movement arguments changed")
		}
		events = append(events, "move")
		return errors.New("synthetic movement failure")
	}, func(options ...interface{}) error {
		if !reflect.DeepEqual(options, args) {
			t.Fatal("click arguments changed")
		}
		events = append(events, "click")
		return errors.New("synthetic click failure")
	}, func(delay int) { events = append(events, fmt.Sprintf("wait:%d", delay)) })
	if want := []string{"move", "wait:50", "click"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestLegacyMovesClickContinuesAfterFalseWithoutSmoothOptions(t *testing.T) {
	var events []string
	args := []interface{}{"middle"}
	movesClickWith(12, 34, args, func(x, y int, options ...interface{}) bool {
		if x != 12 || y != 34 || len(options) != 0 {
			t.Fatal("legacy smooth movement must retain its default options")
		}
		events = append(events, "smooth")
		return false
	}, func(options ...interface{}) error {
		if !reflect.DeepEqual(options, args) {
			t.Fatal("click arguments changed")
		}
		events = append(events, "click")
		return errors.New("synthetic click failure")
	}, func(delay int) { events = append(events, fmt.Sprintf("wait:%d", delay)) })
	if want := []string{"smooth", "wait:50", "click"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

func TestLegacyDragSmoothReleaseAndEarlyFailure(t *testing.T) {
	for _, downFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("downFails=%t", downFails), func(t *testing.T) {
			var events []string
			args := []interface{}{1.0, 2.0, 3}
			dragSmoothWith(12, 34, args, func(options ...interface{}) error {
				if !reflect.DeepEqual(options, []interface{}{"left"}) && !reflect.DeepEqual(options, []interface{}{"left", "up"}) {
					t.Fatalf("toggle arguments = %v", options)
				}
				events = append(events, fmt.Sprint(options))
				if downFails || len(options) == 2 {
					return errors.New("synthetic toggle failure")
				}
				return nil
			}, func(x, y int, options ...interface{}) bool {
				if x != 12 || y != 34 || !reflect.DeepEqual(options, args) {
					t.Fatal("smooth arguments changed")
				}
				events = append(events, "smooth:false")
				return false
			}, func(delay int) { events = append(events, fmt.Sprintf("wait:%d", delay)) })
			want := []string{"[left]", "wait:50", "smooth:false", "[left up]"}
			if downFails {
				want = want[:1]
			}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("events = %v, want %v", events, want)
			}
		})
	}
}

func TestLegacyScrollSmoothContinuesAndReadsFinalDelayLast(t *testing.T) {
	var events []string
	scrollSmoothWith(-4, []int{2, 8, 3}, func(x, y int, options ...int) error {
		if x != 3 || y != -4 || len(options) != 0 {
			t.Fatal("scroll arguments changed")
		}
		events = append(events, "scroll")
		return errors.New("synthetic scroll failure")
	}, func(delay int) { events = append(events, fmt.Sprintf("wait:%d", delay)) }, func() int {
		events = append(events, "read-delay")
		return 17
	})
	want := []string{"scroll", "wait:8", "scroll", "wait:8", "read-delay", "wait:17"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}
