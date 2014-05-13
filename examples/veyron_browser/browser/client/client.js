var veyron = new Veyron(veyronConfig);
var client = veyron.newClient();
var mp;

//TODO(bprosnitz) Remove setup mount. This is used to populate the mount table.
var setupMounts = function() {
  return Promise.all([
    mp.appendToPath('other').mount('/@2@tcp@[::]:111@e73f89469f6ec8252f9e0c7bbe5dd516@1@1@@/FAKE/ADDRESS'),
    mp.appendToPath('recurse').mount(mp.addr),
    mp.appendToPath('a/b/recurse').mount(mp.addr)]);
}

var promiseChain = null;
var init = function() {
  var mtAddr = document.getElementById('mt').value;
  mp = new MountPoint(client, mtAddr);
  promiseChain = setupMounts();
  handleChange();
}

var updateResults = function(query) {
  var results = document.getElementById('results');
  results.innerHTML = '<i>Loading results...</i>';

  promiseChain.then(function() {
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
              if (item.servers.length > 0) {
                // switch mounttable address if this is a remote server.
                a.className = 'remote';
                a.onclick = function() {
                  mt = item.servers[0].server;
                  document.getElementById('mt').value = mt;
                  document.getElementById('query').value = '*';
                  handleChange();
                };
              } else {
                // if this is in the current mounttable, modify the query.
                a.className = 'local';
                a.onclick = function() {
                  document.getElementById('query').value = item.name + '/*';
                  handleChange();
                };
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
  }).catch(function(err) {
    console.error(err);
  });
}

var handleChange = function() {
  updateResults(document.getElementById('query').value);
}
