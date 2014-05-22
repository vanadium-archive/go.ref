var veyron = new Veyron(veyronConfig);
var mountTable = veyron.newMountTable();
var client = veyron.newClient();
var mp;

var init = function() {
  var mtAddr = document.getElementById('mt').value;
  mountTable.then(function(mt) {
    mp = new MountPoint(client, mt, mtAddr);
    handleChange();
  });
}

var updateResults = function(query) {
  var results = document.getElementById('results');
  results.innerHTML = '<i>Loading results...</i>';

  // perform query.
  mp.glob(query).then(function(items) {
    // generate the results list.
    var list = document.createElement('ul');
    for (var i in items) {
      var item = items[i];
      var li = document.createElement('li');
      var a = document.createElement('a');
      
      a.innerHTML = item.name + ' - ' + JSON.stringify(item);
      (function(item, a) {
        isMounttable(client, item).then(function(isMt) {
          if (!isMt) {
            a.className = "service";
          } else {
            a.href = '#';
            a.onclick = function() {
              mp = mp.appendToPath(item.name);
              handleChange();
            };
            if (item.servers.length > 0) {
              a.className = 'remote';
            } else {
              a.className = 'local';
            }
          }
        }, function(reason) {
          console.error('Error testing mounttable: ', reason);
          a.className = "error";
        });
      })(item, a);
      
      li.appendChild(a);
      list.appendChild(li);
    }
    results.innerHTML = '';
    results.appendChild(list);
  }).catch(function(msg) {
    console.error('Problem loading page: ' + msg);
  });
}

var handleChange = function() {
  document.getElementById('mt').value = mp.name;
  updateResults(document.getElementById('query').value);
}
