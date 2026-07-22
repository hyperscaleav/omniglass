# Why there is no test replaying the membership backfill

`20260722120100_system_member_backfill.sql` derived membership from the two places
it used to be implied: `role_assignment` and the old `component.system_id` pointer.
It had a test that executed the shipped migration file rather than a copy, which was
the right shape at the time.

That test was removed in #351, and the reasoning is worth keeping rather than
leaving the absence unexplained.

The migration is **historical**. It runs once, in order, against the schema of its
own moment: `system_member` with text columns, and a `component.system_id` that has
since been dropped. Replaying it against today's schema meant reconstructing that
moment, and the reconstruction kept growing. It already restored one dropped column;
after the owner arcs became uuids it would also have to restore two generations of
column *types*. At that point the test is asserting things about a schema that no
longer exists anywhere, which is archaeology rather than coverage.

What actually guarantees the backfill is correct:

- **Ordering.** Every integration test runs against a fresh database with the whole
  migration chain applied in sequence, so the backfill executes in its own context,
  on the schema it was written for, on every single test run. A break in that chain
  fails everything, loudly.
- **The outcome, not the mechanism.** Membership is asserted directly by the
  membership suite: many-valued memberships, the first one taking the default, the
  default moving rather than duplicating, and assignment creating the binding.

If a future migration needs the same "execute the shipped file" treatment, do it
while the schema it targets is still current, and expect the test to have a short
life for exactly this reason.
