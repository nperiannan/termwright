// Ask for a VLAN id, show it on the current device and report if it is missing.
var vlan = prompt("VLAN id to check", "10");
if (vlan !== null) {
  term.clear();
  term.sendLine("show vlan " + vlan);
  var out = term.readUntil(/[#>] ?$/m, 15000);
  if (out === null) log("No prompt seen within 15 s");
  else if (/not found|invalid|error/i.test(out)) log("VLAN " + vlan + " is NOT configured");
  else log("VLAN " + vlan + " is present");
}
