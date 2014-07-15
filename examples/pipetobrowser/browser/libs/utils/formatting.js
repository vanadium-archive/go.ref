import { default as humanize } from 'npm:humanize'

export function formatDate(d) {
  if(d === undefined || d == null) { return; }
  var naturalDay = humanize.naturalDay(d.getTime() / 1000);
  var naturalTime = humanize.date('g:i a', d);
  return naturalDay + ' at ' + naturalTime;
}

export function formatRelativeTime(d) {
  if(d === undefined || d == null) { return; }
  return humanize.relativeTime(d.getTime() / 1000);
}

export function formatInteger(n) {
  if(n === undefined || n == null) { return; }
  return humanize.numberFormat(n, 0);
}

export function formatBytes(b) {
  if(b === undefined || b == null) { return; }
  return humanize.filesize(b);
}