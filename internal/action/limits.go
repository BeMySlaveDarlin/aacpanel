package action

const (
	idMax     = 64
	targetMax = 256
	// TextMax is the length ceiling for a message sent into a session.
	TextMax     = 50_000
	fileNameMax = 96
	// AskTextMax is the length ceiling for a free-form answer to a session question.
	AskTextMax  = 4_000
	workLineMax = 200
	pathMax     = 4096
	launchMax   = 8 << 10
)

const (
	askQuestionsMax = 8
	askOptionsMax   = 12
)

// FileMax is the size ceiling for one file from the phone, in bytes.
const FileMax = 32 << 20

// FilesBytesMax is the weight ceiling for a whole batch, in bytes.
const FilesBytesMax = FileMax

// FilesMax is how many files travel at once.
const FilesMax = 16
