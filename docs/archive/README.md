# Archived Backlog Documentation

This directory contains frozen snapshots of the original backlog planning documents from the early phases of the CUDA Learning Platform project.

## Relationship to `docs/backlog/`

The four files in this directory are archived, read-only copies. The corresponding **active** versions live in `docs/backlog/`, which also contains additional planning documents that were created after the initial backlog migration. The full set of backlog files is:

| File | `docs/archive/` | `docs/backlog/` |
|------|:---:|:---:|
| `infrastructure.md` | frozen copy | active |
| `kernels.md` | frozen copy | active |
| `neural-networks.md` | frozen copy | active |
| `video-streaming.md` | frozen copy | active |
| `webrtc-session-lifecycle-hardening.md` | -- | active |
| `webrtc-trickle-ice-inline.md` | -- | active |

## Purpose

These archived files document the project's evolution from markdown-based backlog management to structured GitHub Issues tracking. They are preserved as a historical record to show:

- How project scope was initially defined and organized
- The learning journey and progression planning
- Evolution of feature ideas and implementation approaches
- Initial task breakdown and organization

## Migration Status

All pending backlog items from these files have been migrated to [GitHub Issues](https://github.com/josnelihurt-code/learning-cuda/issues) as part of the project's transition to structured issue tracking. The migration process:

1. Analyzed ~330+ individual backlog items
2. Applied automated backlog grooming to group related tasks
3. Created 27 refined, actionable GitHub Issues (#503-529)
4. Preserved completed items with their original issue numbers (#4-169)

## Files

- **`infrastructure.md`**: Infrastructure, DevOps, observability, testing, and deployment planning
- **`kernels.md`**: Image processing kernel roadmap organized by filter family and complexity
- **`neural-networks.md`**: Neural network learning path from CUDA basics to production frameworks
- **`video-streaming.md`**: Video streaming optimization research and POC planning

Each file includes:
- Historical task breakdowns organized by category
- Mappings to current GitHub Issues where applicable
- Learning goals and progression notes
- Original planning rationale

## Broader Documentation Structure

The `docs/` directory contains additional documentation outside this archive:

```
docs/
  archive/                              # This directory (frozen historical backlog)
  backlog/                              # Active backlog planning documents
    webrtc-session-lifecycle-hardening.md
    webrtc-trickle-ice-inline.md
    infrastructure.md, kernels.md, neural-networks.md, video-streaming.md
  issues/                               # Issue investigation records
    registry-issue-go4.6.1-cpp4.7.9-proto4.7.0.md
  runbooks/                             # Operational runbooks
    accelerator-mtls.md                 # mTLS certificate setup and rotation
  uml/                                  # UML class diagram docs and generated output
    README.md, index.md
    generated/                          # clang-uml .mmd diagrams (not committed)
  ci-workflows.md                       # CI/CD pipeline documentation
  testing-and-coverage.md               # Testing strategy and coverage
```

## Current Project Management

**Active backlog**: See [GitHub Issues](https://github.com/josnelihurt-code/learning-cuda/issues)

All new tasks, features, and planning should be managed through GitHub Issues with proper labels, milestones, and project boards.

## Notes

These files are **read-only** and maintained for historical reference only. They are not actively maintained or updated. For current project status and planning, refer to GitHub Issues or the active copies in `docs/backlog/`.
