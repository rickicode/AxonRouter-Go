import { describe, it, expect } from 'vitest';
import { planStepSync, type ExistingStep, type StepDraft } from '../combo-modal-helpers';

function ex(id: string, model_id: string, priority = 1, weight = 100): ExistingStep {
	return { id, model_id, priority, weight };
}

function draft(model_id: string, priority = 1, weight = 100, id?: string): StepDraft {
	return { id, model_id, priority, weight };
}

describe('planStepSync', () => {
	it('does nothing when drafts match existing', () => {
		const existing = [ex('s1', 'a'), ex('s2', 'b')];
		const drafts = [draft('a', 1, 100, 's1'), draft('b', 1, 100, 's2')];
		const plan = planStepSync(existing, drafts);
		expect(plan.toRemove).toEqual([]);
		expect(plan.toAdd).toEqual([]);
	});

	it('removes steps missing from drafts', () => {
		const existing = [ex('s1', 'a'), ex('s2', 'b')];
		const drafts = [draft('a', 1, 100, 's1')];
		const plan = planStepSync(existing, drafts);
		expect(plan.toRemove).toEqual(['s2']);
		expect(plan.toAdd).toEqual([]);
	});

	it('adds new steps without ids', () => {
		const existing = [ex('s1', 'a')];
		const drafts = [draft('a', 1, 100, 's1'), draft('c', 2, 50)];
		const plan = planStepSync(existing, drafts);
		expect(plan.toRemove).toEqual([]);
		expect(plan.toAdd).toEqual([{ model_id: 'c', priority: 2, weight: 50 }]);
	});

	it('removes then re-adds changed existing steps', () => {
		const existing = [ex('s1', 'a', 1, 100)];
		const drafts = [draft('a', 2, 100, 's1')];
		const plan = planStepSync(existing, drafts);
		expect(plan.toRemove).toEqual(['s1']);
		expect(plan.toAdd).toEqual([{ model_id: 'a', priority: 2, weight: 100 }]);
	});
});
