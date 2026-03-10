# Git Conventions

## 1. Core Summary

PicoClaw follows Conventional Commits with squash merge workflow. The main development branch is `dev`. All changes must go through PR with CI checks and maintainer approval. AI-generated code disclosure is mandatory.

## 2. Branch Strategy

### Long-Lived Branches

| Branch | Purpose |
|--------|---------|
| `dev` | Active development, PRs target this branch |
| `main` | Stable branch (protected) |
| `release/x.y` | Release branches, critical fixes only |

### Branch Naming

```
feat/description    # New features
fix/description     # Bug fixes
docs/description    # Documentation
refactor/description # Code refactoring
```

## 3. Commit Message Format

Follows [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): description
```

### Types

| Type | Usage |
|------|-------|
| `feat` | New feature |
| `fix` | Bug fix |
| `docs` | Documentation |
| `chore` | Maintenance, deps |
| `refactor` | Code refactoring |

### Examples

```
feat(channel): add wecom official template card replies
fix(session): resolve memory leak in history compression
docs(readme): update installation instructions
chore(deps): bump github.com/modelcontextprotocol/go-sdk
```

## 4. PR Workflow

1. Fork repo and create branch from `dev`
2. Make changes, run `make check` locally
3. Open PR with complete template (including AI disclosure)
4. Wait for CI pass + maintainer approval
5. Maintainer performs squash merge

### PR Requirements

- CI must pass (`make check`)
- At least one maintainer approval
- Complete PR template with AI code generation disclosure
- No unresolved review comments

## 5. Source of Truth

- **Contributing Guide:** `CONTRIBUTING.md` - Full contribution guidelines
- **PR Template:** `.github/pull_request_template.md` - Required PR sections
- **Conventional Commits:** https://www.conventionalcommits.org/
