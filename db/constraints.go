package db

import (
	"fmt"
	"strings"
	"time"
)

// Constraint is one operator-authored operation constraint for a task, free-text,
// stored in task_constraints keyed by exploration_id (cascades with the exploration).
// Three kinds:
//   - allow — permitted operations (frames what may be explored)
//   - deny  — forbidden operations (frames what must not be touched)
//   - override — the operator's explicit authorization for THIS task. It outranks both
//     above when they conflict. Only humans write it (the main agent transcribing an
//     operator instruction, or a human via 总览「约束管理」); the goals decomposer must
//     never synthesize one, since that would let the model authorize itself.
type Constraint struct {
	ID        int64     `json:"id"`
	Kind      string    `json:"kind"` // allow | deny | override
	Text      string    `json:"text"`
	Origin    string    `json:"origin,omitempty"` // goals | human | system
	CreatedAt time.Time `json:"created_at"`
}

// ConstraintKinds are the accepted kind values. override is deliberately last: it is
// the operator override, not a boundary the decomposer may infer.
var ConstraintKinds = []string{"allow", "deny", "override"}

// ValidConstraintKind reports whether kind is an accepted constraint kind.
func ValidConstraintKind(kind string) bool {
	for _, k := range ConstraintKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// ListConstraints returns this exploration's constraints ordered override → allow →
// deny, oldest first within each group. Render order matters: the prompt block puts the
// operator's authorizations FIRST so they are the most recent thing read before the
// allow/deny lists, and so the UI shows the authoritative items on top.
func (s *ExplorationStore) ListConstraints() ([]Constraint, error) {
	rows, err := s.db.Query(`
SELECT id, kind, text, COALESCE(origin,''), created_at
FROM task_constraints WHERE exploration_id=$1
ORDER BY CASE kind WHEN 'override' THEN 0 WHEN 'allow' THEN 1 ELSE 2 END, id`, s.expID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Constraint
	for rows.Next() {
		var c Constraint
		if err := rows.Scan(&c.ID, &c.Kind, &c.Text, &c.Origin, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddConstraint inserts one constraint (kind must be allow|deny|override) and returns
// its id.
func (s *ExplorationStore) AddConstraint(kind, text, origin string) (int64, error) {
	if !ValidConstraintKind(kind) {
		return 0, fmt.Errorf("kind 必须是 %s", strings.Join(ConstraintKinds, " / "))
	}
	if origin == "" {
		origin = "system"
	}
	var id int64
	err := s.db.QueryRow(`
INSERT INTO task_constraints(exploration_id, kind, text, origin)
VALUES ($1, $2, $3, $4) RETURNING id`, s.expID, kind, text, origin).Scan(&id)
	return id, err
}

// UpdateConstraint rewrites a constraint's kind + text; scoped to this exploration.
// Returns an error if no such constraint exists.
func (s *ExplorationStore) UpdateConstraint(id int64, kind, text string) error {
	if !ValidConstraintKind(kind) {
		return fmt.Errorf("kind 必须是 %s", strings.Join(ConstraintKinds, " / "))
	}
	res, err := s.db.Exec(`
UPDATE task_constraints SET kind=$1, text=$2, updated_at=now()
WHERE id=$3 AND exploration_id=$4`, kind, text, id, s.expID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("约束不存在")
	}
	return nil
}

// DeleteConstraint removes a constraint; scoped to this exploration. Returns an
// error if no such constraint exists.
func (s *ExplorationStore) DeleteConstraint(id int64) error {
	res, err := s.db.Exec(`DELETE FROM task_constraints WHERE id=$1 AND exploration_id=$2`, id, s.expID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("约束不存在")
	}
	return nil
}
