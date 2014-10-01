package impl

// NOTE(caprita): The jenkins user id on Cos's Mac is 498.  We need to either
// lower the threshold just for the tests to pass, or default the Mac threshold
// to 498 or lower.  For now, do the latter.
const uidThreshold = 498
