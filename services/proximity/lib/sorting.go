package proximity

type readingSorter struct {
	readings []ScanReading
	by       func(r1, r2 ScanReading) bool
}

func (s *readingSorter) Len() int {
	return len(s.readings)
}

func (s *readingSorter) Swap(i, j int) {
	s.readings[i], s.readings[j] = s.readings[j], s.readings[i]
}

func (s *readingSorter) Less(i, j int) bool {
	return s.by(s.readings[i], s.readings[j])
}

type deviceSorter struct {
	devices []device
	by      func(d1, d2 device) bool
}

func (s *deviceSorter) Len() int {
	return len(s.devices)
}

func (s *deviceSorter) Swap(i, j int) {
	s.devices[i], s.devices[j] = s.devices[j], s.devices[i]
}

func (s *deviceSorter) Less(i, j int) bool {
	return s.by(s.devices[i], s.devices[j])
}
