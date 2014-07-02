export function gitStatusSearch(item, keyword) {
  if (!keyword) {
    return true;
  }

  // we only search file
  if (item.file.indexOf(keyword) >= 0) {
    return true
  }

  return false;
};