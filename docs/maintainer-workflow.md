# Maintainer guide

Welcome to the maintainer guide for our project!
As a project maintainer, you play a crucial role in ensuring the success and health of the project.
Below are key responsibilities and guidelines to help you manage the project effectively.

## Key responsibilities

### Manage Issues
1. Issue Triage
    - Establish a regular schedule for reviewing and triaging open issues.
    - Close irrelevant issues, and categorize and prioritize others using milestones.

1. Acknowledging an Issue
    - Review new issues promptly to address major blockers.
    - Leave a comment on the issue to show appreciation for the creator's time and efforts.
    - Request additional details if the issue description is unclear.

1. Labelling Issues
    - Add appropriate labels to convey the status of each issue.
    - Stalebot ignores issues labeled `awaiting-inputs`, `work-in-progress`, and `help-wanted`. See [automation docs](docs/automation.md)
    - Guidelines for specific labels:
        - `help-wanted`: Use for issues that we want to work on but haven't been picked up.
        - `work-in-progress`: Indicate ongoing work without a corresponding pull request.
        - `wontfix`/`invalid`: Always add with a clear explanation.

1. Assigning an Issue
    - Assign issues to contributors who show interest or agree to work on them.
    - If someone has started working on an issue, assign it to them for visibility.

### Manage Pull Requests (PRs)
1. PR review
    - Hold off on reviewing until the contributor indicates readiness, unless explicitly asked otherwise.
    - Label PRs with `awaiting-inputs` if help is needed to understand the changes.
    - Ensure that new features or changes are properly documented.
    - Ensure that adequate tests are added (or already exists), create a separate issue if required.

1. PR approval and merge
    - Contributors cannot merge PRs, assist them in the merging process.
    - Ensure Continuous Integration (CI) passes before merging, except for non-code/test changes.
    - Approve CI builds for first time contributors.

1. Labeling PRs
    - Stalebot ignores PRs labeled `awaiting-inputs`, `work-in-progress`, and `help-wanted`.
    - It's a good practice to add appropriate labels on PRs.

### Manage Releases
- Choose the correct tag based on major, minor, or patch fixes using [semver versioning](https://semver.org/).
- Aim to create releases promptly to minimize user wait times.
- If possible, update releases with informative release notes highlighting breaking changes, major features, and bug fixes.

### Arrange and Drive Community Meetings
- Attend community meetings to engage with contributors and users.
- Promptly address action items from meeting notes.
- Foster a welcoming, open-minded, respectful, and friendly environment.

### Ensure Code of Conduct Compliance
- Familiarize yourself with the [CNCF code of conduct](https://www.cncf.io/conduct/).
- Enforce the code of conduct among contributors and maintainers, taking necessary actions when required.

### Keeping the Repository Up to Date
- Periodically review documentation and create issues for any missing pieces.
- Implement automation to reduce inconsistencies and enhance productivity.

## Reading materials
For further insights, refer to these valuable resources:
- [Best practices for maintainers](https://opensource.guide/best-practices)
- [Practical skills for maintainers](https://www.freecodecamp.org/news/practical-skills-for-open-source-maintainers)
- [Becoming an open source maintainer](https://kentcdodds.com/blog/becoming-an-open-source-project-maintainer)

**Thank you for your dedication to maintaining our project!**
