package build

type OperatingSystem uint8

const (
	LINUX OperatingSystem = iota
	DARWIN
	WINDOWS
)

func (os OperatingSystem) String() string {
	switch os {
	case LINUX:
		return "linux"
	case DARWIN:
		return "darwin"
	case WINDOWS:
		return "windows"
	default:
		return "unknown"
	}
}

type Format uint8

const (
	ELF Format = iota
	MACH
	PE
)

func (format Format) String() string {
	switch format {
	case ELF:
		return "elf"
	case MACH:
		return "mach-o"
	case PE:
		return "pe"
	default:
		return "unknown"
	}
}

type Architecture uint8

const (
	AMD64 Architecture = iota
	ARM
	X86
)

func (arch Architecture) String() string {
	switch arch {
	case AMD64:
		return "amd64"
	case ARM:
		return "arm"
	case X86:
		return "x86"
	default:
		return "unknown"
	}
}
