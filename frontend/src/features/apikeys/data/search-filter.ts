interface APIKeySearchOptions {
  canViewUsers: boolean;
  includeKey?: boolean;
}

// Keep the search OR separate from project, status, type and owner constraints.
export function buildAPIKeySearchFilter(search: string | undefined, { canViewUsers, includeKey = false }: APIKeySearchOptions) {
  const term = search?.trim();
  if (!term) return {};

  const or: Array<
    { nameContainsFold: string } | { keyContainsFold: string } | { hasUserWith: Array<{ nameContainsFold: string }> }
  > = [{ nameContainsFold: term }];

  if (includeKey) or.push({ keyContainsFold: term });
  if (canViewUsers) or.push({ hasUserWith: [{ nameContainsFold: term }] });

  return { or };
}
