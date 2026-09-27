package netcode

import (
	"math"
	"sort"
)

type Point struct {
	ID   uint64
	X, Z float64
}
type cell struct{ X, Z int64 }
type SpatialIndex struct {
	size  float64
	cells map[cell][]Point
}

func NewSpatialIndex(size float64) *SpatialIndex {
	if size <= 0 || math.IsNaN(size) || math.IsInf(size, 0) {
		panic("invalid cell size")
	}
	return &SpatialIndex{size: size, cells: make(map[cell][]Point)}
}
func (s *SpatialIndex) cell(x, z float64) cell {
	return cell{int64(math.Floor(x / s.size)), int64(math.Floor(z / s.size))}
}
func (s *SpatialIndex) Rebuild(points []Point) {
	clear(s.cells)
	for _, p := range points {
		key := s.cell(p.X, p.Z)
		s.cells[key] = append(s.cells[key], p)
	}
}

// The returned set is a complete visibility baseline. Leaving it is not death.
// Existing members use a larger exit radius to avoid boundary churn.
func (s *SpatialIndex) Select(self Point, enter, exit float64, limit int, previous map[uint64]bool) []uint64 {
	if limit < 1 || enter < 0 || exit < enter || exit/s.size > 64 {
		return nil
	}
	low, high := s.cell(self.X-exit, self.Z-exit), s.cell(self.X+exit, self.Z+exit)
	type candidate struct {
		id       uint64
		distance float64
	}
	candidates := []candidate{}
	for x := low.X; x <= high.X; x++ {
		for z := low.Z; z <= high.Z; z++ {
			for _, p := range s.cells[cell{x, z}] {
				if p.ID == self.ID {
					continue
				}
				dx, dz := p.X-self.X, p.Z-self.Z
				distance := dx*dx + dz*dz
				radius := enter
				if previous[p.ID] {
					radius = exit
				}
				if distance <= radius*radius {
					candidates = append(candidates, candidate{p.ID, distance})
				}
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].distance == candidates[j].distance {
			return candidates[i].id < candidates[j].id
		}
		return candidates[i].distance < candidates[j].distance
	})
	result := []uint64{self.ID}
	for _, c := range candidates {
		if len(result) == limit {
			break
		}
		result = append(result, c.id)
	}
	return result
}
