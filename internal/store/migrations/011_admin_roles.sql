-- Admin roles and account disabling.
--
-- Every pre-existing admin was a full administrator, so the column default is
-- the least-privileged role and the existing rows are promoted to owner
-- explicitly. New local admins default to editor unless their creator selects
-- owner.
ALTER TABLE admins ADD COLUMN role TEXT NOT NULL DEFAULT 'editor';
ALTER TABLE admins ADD COLUMN disabled_at TEXT;
UPDATE admins SET role = 'owner';

-- session_ref gives the session list a stable, non-secret handle for revoking
-- one session. Pre-migration rows keep an empty reference and can only be
-- removed by revoke-all.
ALTER TABLE admin_sessions ADD COLUMN session_ref TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX admin_sessions_session_ref
    ON admin_sessions(session_ref)
    WHERE session_ref <> '';
