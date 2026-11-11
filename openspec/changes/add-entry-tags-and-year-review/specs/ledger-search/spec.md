## MODIFIED Requirements

### Requirement: Inclusive date-range filters
`GET /entries` and `GET /api/v1/entries` SHALL accept `from` and `to` parameters in `YYYY-MM-DD` form and return entries whose `created_at` falls on or after `from` and on or before `to`, in addition to the existing `year` filter. Every list filter (`q`, `from`, `to`, `year`, `photo`, `person` and `tag`) SHALL combine by intersection, and a `tag` parameter SHALL filter on the tag capability defined by `entry-tags` without entering the `q` grammar, so tags never change how a query is parsed or matched.

#### Scenario: Both boundaries are inclusive
- **WHEN** a client requests `from=2024-01-01&to=2024-01-31` and entries exist on both of those dates
- **THEN** both boundary entries are included in the result

#### Scenario: Filter combined with year
- **WHEN** a client supplies `year=2024&from=2024-06-01`
- **THEN** only entries from 1 June 2024 onwards are returned

#### Scenario: Malformed date
- **WHEN** a client sends `from=2024-13-01` to `GET /api/v1/entries`
- **THEN** the response is 400 with the error code `invalid_date`

#### Scenario: One boundary only
- **WHEN** a client supplies `from` and no `to`
- **THEN** every entry on or after that date is returned, with no upper bound

#### Scenario: Tag intersects with a date range
- **WHEN** a client supplies `year=2024&to=2024-06-30&tag=misheard`
- **THEN** only entries carrying `misheard` that fall between 1 January and 30 June 2024 are returned

#### Scenario: Tag does not change the query grammar
- **WHEN** a client sends `q=feature -feutenreus&tag=typo`
- **THEN** the query is still parsed as a required term with an exclusion and the tag is applied as an additional filter on the result
