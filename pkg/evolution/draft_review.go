package evolution

type DraftReviewResult struct {
	Status      DraftStatus
	Findings    []string
	ReviewNotes []string
}

func ReviewDraft(draft SkillDraft) DraftReviewResult {
	return ReviewDraftWithPolicy(draft, 0)
}
