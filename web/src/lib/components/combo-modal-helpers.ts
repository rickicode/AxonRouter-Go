export interface StepDraft {
	id?: string;
	model_id: string;
	priority: number;
	weight: number;
}

export interface ExistingStep {
	id: string;
	model_id: string;
	priority: number;
	weight: number;
}

export interface StepSyncPlan {
	toRemove: string[];
	toAdd: Omit<StepDraft, 'id'>[];
}

export function planStepSync(existing: ExistingStep[], drafts: StepDraft[]): StepSyncPlan {
	const toRemove: string[] = [];
	const toAdd: Omit<StepDraft, 'id'>[] = [];
	const draftById = new Map(drafts.filter((d) => d.id).map((d) => [d.id!, d]));

	for (const ex of existing) {
		const draft = draftById.get(ex.id);
		if (!draft) {
			toRemove.push(ex.id);
		} else if (
			draft.model_id !== ex.model_id ||
			draft.priority !== ex.priority ||
			draft.weight !== ex.weight
		) {
			toRemove.push(ex.id);
		}
	}

	for (const draft of drafts) {
		if (!draft.id) {
			toAdd.push({ model_id: draft.model_id, priority: draft.priority, weight: draft.weight });
			continue;
		}
		const ex = existing.find((e) => e.id === draft.id);
		if (
			!ex ||
			ex.model_id !== draft.model_id ||
			ex.priority !== draft.priority ||
			ex.weight !== draft.weight
		) {
			toAdd.push({ model_id: draft.model_id, priority: draft.priority, weight: draft.weight });
		}
	}

	return { toRemove, toAdd };
}
