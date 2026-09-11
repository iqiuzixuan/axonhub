import { type ClassValue, clsx } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export const extractNumberID = (id: string) => {
  const lastSlashIndex = id.lastIndexOf('/');
  return id.slice(lastSlashIndex + 1);
};

export const extractNumberIDAsNumber = (id: string) => {
  return Number(extractNumberID(id));
};

export const buildGUID = (type: string, id: string) => {
  return `gid://axonhub/${type}/${id}`;
};

// Display the saved name in the user's chosen order, including internal spaces.
export function formatUserName(name?: string | null, email?: string | null) {
  return name?.trim() || email || '';
}

export function userNameInitials(name?: string | null, email?: string | null) {
  return Array.from(formatUserName(name, email)).slice(0, 2).join('').toUpperCase() || 'U';
}

// Omit unchanged names so unrelated edits also work for migrated, longer names.
export function userNameUpdate(name: string, originalName?: string): { name?: string } {
  return name === originalName ? {} : { name };
}
