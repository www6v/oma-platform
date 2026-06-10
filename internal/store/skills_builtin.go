package store

// BuiltinSkill is a read-only catalog entry served on every tenant.
type BuiltinSkill struct {
	ID            string
	DisplayTitle  string
	Name          string
	Description   string
	Source        string
	LatestVersion string
	CreatedAt     int64
}

var builtinSkills = []BuiltinSkill{
	{
		ID:            "builtin_xlsx",
		DisplayTitle:  "Excel (.xlsx) Processing",
		Name:          "xlsx",
		Description:   "Read, analyze, and transform Excel spreadsheets. Extracts sheets, rows, and cell data from .xlsx files.",
		Source:        "builtin",
		LatestVersion: "1",
		CreatedAt:     1735689600000,
	},
	{
		ID:            "builtin_pdf",
		DisplayTitle:  "PDF Processing",
		Name:          "pdf",
		Description:   "Read and extract text, tables, and metadata from PDF documents.",
		Source:        "builtin",
		LatestVersion: "1",
		CreatedAt:     1735689600000,
	},
	{
		ID:            "builtin_pptx",
		DisplayTitle:  "PowerPoint (.pptx) Processing",
		Name:          "pptx",
		Description:   "Read and extract text, slides, and metadata from PowerPoint presentations.",
		Source:        "builtin",
		LatestVersion: "1",
		CreatedAt:     1735689600000,
	},
	{
		ID:            "builtin_docx",
		DisplayTitle:  "Word (.docx) Processing",
		Name:          "docx",
		Description:   "Read and extract text, tables, and metadata from Word documents.",
		Source:        "builtin",
		LatestVersion: "1",
		CreatedAt:     1735689600000,
	},
}

// BuiltinSkillByID returns a built-in skill or nil.
func BuiltinSkillByID(id string) *BuiltinSkill {
	for i := range builtinSkills {
		if builtinSkills[i].ID == id {
			s := builtinSkills[i]
			return &s
		}
	}
	return nil
}

// BuiltinSkillCount returns the static catalog size.
func BuiltinSkillCount() int {
	return len(builtinSkills)
}

// ListBuiltinSkills returns a copy of the built-in catalog.
func ListBuiltinSkills() []BuiltinSkill {
	out := make([]BuiltinSkill, len(builtinSkills))
	copy(out, builtinSkills)
	return out
}

// IsBuiltinSkillID reports whether id refers to a built-in skill.
func IsBuiltinSkillID(id string) bool {
	return BuiltinSkillByID(id) != nil
}
