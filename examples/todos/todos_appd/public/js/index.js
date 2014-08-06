var app = app || {};

(function() {
  'use strict';

  ////////////////////////////////////////
  // Helpers

  var d = new app.Dispatcher();

  var activateInput = function(input) {
    input.focus();
    input.select();
  };

  var okCancelEvents = function(callbacks) {
    var ok = callbacks.ok || function() {};
    var cancel = callbacks.cancel || function() {};
    var done = function(ev) {
      var value = ev.target.value;
      if (value) {
        ok(value, ev);
      } else {
        cancel(ev);
      }
    };
    return {
      onKeyDown: function(ev) {
        if (ev.which === 27) {  // esc
          cancel(ev);
        }
      },
      onKeyUp: function(ev) {
        if (ev.which === 13) {  // enter
          done(ev);
        }
      },
      onBlur: function(ev) {
        done(ev);
      }
    };
  };

  ////////////////////////////////////////
  // Components

  var TagFilter = React.createClass({
    displayName: 'TagFilter',
    render: function() {
      var that = this;
      var tagFilter = this.props.tagFilter;
      var tagInfos = [], totalCount = 0;
      _.each(this.props.todos, function(todo) {
        _.each(todo.tags, function(tag) {
          var tagInfo = _.find(tagInfos, function(x) {
            return x.tag === tag;
          });
          if (!tagInfo) {
            tagInfos.push({tag: tag, count: 1, selected: tagFilter === tag});
          } else {
            tagInfo.count++;
          }
        });
        totalCount++;
      });
      tagInfos = _.sortBy(tagInfos, function(x) { return x.tag; });
      // Note, the 'All items' tag handling is fairly convoluted in Meteor.
      tagInfos.unshift({
        tag: null,
        count: totalCount,
        selected: tagFilter === null
      });

      var children = [];
      _.each(tagInfos, function(tagInfo) {
        var count = React.DOM.span(
          {className: 'count'}, '(' + tagInfo.count + ')');
        children.push(React.DOM.div({
          className: 'tag' + (tagInfo.selected ? ' selected' : ''),
          onMouseDown: function() {
            var newTagFilter = tagFilter === tagInfo.tag ? null : tagInfo.tag;
            that.props.setTagFilter(newTagFilter);
          }
        }, tagInfo.tag === null ? 'All items' : tagInfo.tag, ' ', count));
      });
      return React.DOM.div(
        {id: 'tag-filter', className: 'tag-list'},
        React.DOM.div({className: 'label'}, 'Show:'),
        children);
    }
  });

  var Tags = React.createClass({
    displayName: 'Tags',
    getInitialState: function() {
      return {
        addingTag: false
      };
    },
    componentDidUpdate: function() {
      if (this.state.addingTag) {
        activateInput(this.getDOMNode().querySelector('#edittag-input'));
      }
    },
    render: function() {
      var that = this;
      var children = [];
      _.each(this.props.tags, function(tag) {
        // Note, we must specify the "key" prop so that React doesn't reuse the
        // opacity=0 node after a tag is removed.
        children.push(React.DOM.div(
          {className: 'tag removable_tag', key: tag},
          React.DOM.div({className: 'name'}, tag),
          React.DOM.div({
            className: 'remove',
            onClick: function(ev) {
              ev.target.parentNode.style.opacity = 0;
              // Wait for CSS animation to finish.
              window.setTimeout(function() {
                d.removeTag(that.props.todoId, tag);
              }, 300);
            }
          })));
      });
      if (this.state.addingTag) {
        children.push(React.DOM.div(
          {className: 'tag edittag'},
          React.DOM.input(_.assign({
            type: 'text',
            id: 'edittag-input',
            defaultValue: ''
          }, okCancelEvents({
            ok: function(value) {
              d.addTag(that.props.todoId, value);
              that.setState({addingTag: false});
            },
            cancel: function() {
              that.setState({addingTag: false});
            }
          })))));
      } else {
        children.push(React.DOM.div({
          className: 'tag addtag',
          onClick: function() {
            that.setState({addingTag: true});
          }
        }, '+tag'));
      }
      return React.DOM.div({className: 'item-tags'}, children);
    }
  });

  var Todo = React.createClass({
    displayName: 'Todo',
    getInitialState: function() {
      return {
        editingText: false
      };
    },
    componentDidUpdate: function() {
      if (this.state.editingText) {
        activateInput(this.getDOMNode().querySelector('#todo-input'));
      }
    },
    render: function() {
      var that = this;
      var todo = this.props.todo, children = [];
      if (this.state.editingText) {
        children.push(React.DOM.div(
          {className: 'edit'},
          React.DOM.input(_.assign({
            id: 'todo-input',
            type: 'text',
            defaultValue: todo.text
          }, okCancelEvents({
            ok: function(value) {
              d.editTodoText(todo._id, value);
              that.setState({editingText: false});
            },
            cancel: function() {
              that.setState({editingText: false});
            }
          })))));
      } else {
        children.push(React.DOM.div({
          className: 'destroy',
          onClick: function() {
            d.removeTodo(todo._id);
          }
        }));
        children.push(React.DOM.div(
          {className: 'display'},
          React.DOM.input({
            className: 'check',
            name: 'markdone',
            type: 'checkbox',
            checked: todo.done,
            onClick: function() {
              d.markTodoDone(!todo.done);
            }
          }),
          React.DOM.div({
            className: 'todo-text',
            onDoubleClick: function() {
              that.setState({editingText: true});
            }
          }, todo.text)));
      }
      children.push(new Tags({todoId: todo._id, tags: todo.tags}));
      return React.DOM.li({
        className: 'todo' + (todo.done ? ' done' : '')
      }, children);
    }
  });

  var Todos = React.createClass({
    displayName: 'Todos',
    render: function() {
      var that = this;
      if (this.props.listId === null) {
        return null;
      }
      var children = [];
      if (this.props.todos === null) {
        children.push('Loading...');
      } else {
        var tagFilter = this.props.tagFilter, items = [];
        _.each(this.props.todos, function(todo) {
          if (tagFilter === null || _.contains(todo.tags, tagFilter)) {
            items.push(new Todo({todo: todo}));
          }
        });
        children.push(React.DOM.div(
          {id: 'new-todo-box'},
          React.DOM.input(_.assign({
            type: 'text',
            id: 'new-todo',
            placeholder: 'New item'
          }, okCancelEvents({
            ok: function(value, ev) {
              var tags = tagFilter ? [tagFilter] : [];
              d.addTodo(that.props.listId, value, tags);
              ev.target.value = '';
            }
          })))));
        children.push(React.DOM.ul({id: 'item-list'}, items));
      }
      return React.DOM.div({id: 'items-view'}, children);
    }
  });

  var List = React.createClass({
    displayName: 'List',
    getInitialState: function() {
      return {
        editingName: false
      };
    },
    componentDidUpdate: function() {
      if (this.state.editingName) {
        activateInput(this.getDOMNode().querySelector('#list-name-input'));
      }
    },
    render: function() {
      var that = this;
      var list = this.props.list, child;
      // http://facebook.github.io/react/docs/forms.html#controlled-components
      if (this.state.editingName) {
        child = React.DOM.div(
          {className: 'edit'},
          React.DOM.input(_.assign({
            className: 'list-name-input',
            id: 'list-name-input',
            type: 'text',
            defaultValue: list.name
          }, okCancelEvents({
            ok: function(value) {
              d.editListName(list._id, value);
              that.setState({editingName: false});
            },
            cancel: function() {
              that.setState({editingName: false});
            }
          }))));
      } else {
        child = React.DOM.div(
          {className: 'display'},
          React.DOM.a({
            className: 'list-name' + (list.name ? '' : ' empty'),
            href: '/' + list._id
          }, list.name));
      }
      return React.DOM.div({
        className: 'list' + (list.selected ? ' selected' : ''),
        onMouseDown: function() {
          that.props.setListId(list._id);
        },
        onClick: function(ev) {
          ev.preventDefault();  // prevent page refresh
        },
        onDoubleClick: function() {
          that.setState({editingName: true});
        }
      }, child);
    }
  });

  var Lists = React.createClass({
    displayName: 'Lists',
    render: function() {
      var that = this;
      var children = [React.DOM.h3({}, 'Todo Lists')];
      if (this.props.lists === null) {
        children.push(React.DOM.div({id: 'lists'}, 'Loading...'));
      } else {
        var lists = [];
        _.each(this.props.lists, function(list) {
          list.selected = that.props.listId === list._id;
          lists.push(new List({
            list: list,
            setListId: that.props.setListId
          }));
        });
        children.push(React.DOM.div({id: 'lists'}, lists));
        children.push(React.DOM.div(
          {id: 'createList'},
          React.DOM.input(_.assign({
            type: 'text',
            id: 'new-list',
            placeholder: 'New list'
          }, okCancelEvents({
            ok: function(value, ev) {
              var id = d.addList(value);
              that.props.setListId(id);
              ev.target.value = '';
            }
          })))));
      }
      return React.DOM.div({}, children);
    }
  });

  var Page = React.createClass({
    displayName: 'Page',
    getInitialState: function() {
      return {
        lists: null,  // all lists
        todos: null,  // all todos for current listId
        listId: this.props.initialListId,  // current list
        tagFilter: null  // current tag
      };
    },
    fetchLists: function() {
      return app.Lists.find({}, {sort: {name: 1}});
    },
    fetchTodos: function(listId) {
      if (listId === null) {
        return null;
      }
      return app.Todos.find({listId: listId}, {sort: {timestamp: 1}});
    },
    updateURL: function() {
      var router = this.props.router, listId = this.state.listId;
      router.navigate(listId === null ? '' : String(listId));
    },
    componentDidMount: function() {
      var that = this;
      var lists = this.fetchLists();
      var listId = this.state.listId;
      if (listId === null && lists.length > 0) {
        listId = lists[0]._id;
      }
      this.setState({
        lists: lists,
        todos: this.fetchTodos(listId),
        listId: listId
      });
      this.updateURL();

      app.Lists.onChange(function() {
        that.setState({lists: that.fetchLists()});
      });
      app.Todos.onChange(function() {
        that.setState({todos: that.fetchTodos(that.state.listId)});
      });
    },
    componentDidUpdate: function() {
      this.updateURL();
    },
    render: function() {
      var that = this;
      return React.DOM.div({}, [
        React.DOM.div({id: 'top-tag-filter'}, new TagFilter({
          todos: this.state.todos,
          tagFilter: this.state.tagFilter,
          setTagFilter: function(tagFilter) {
            that.setState({tagFilter: tagFilter});
          }
        })),
        React.DOM.div({id: 'main-pane'}, new Todos({
          todos: this.state.todos,
          listId: this.state.listId,
          tagFilter: this.state.tagFilter
        })),
        React.DOM.div({id: 'side-pane'}, new Lists({
          lists: this.state.lists,
          listId: this.state.listId,
          setListId: function(listId) {
            if (listId !== that.state.listId) {
              that.setState({
                todos: that.fetchTodos(listId),
                listId: listId
              });
            }
          }
        }))
      ]);
    }
  });

  ////////////////////////////////////////
  // Initialization

  app.init = function() {
    var Router = Backbone.Router.extend({
      routes: {
        '': 'main',
        ':listId': 'main'
      }
    });
    var router = new Router();

    var page;
    router.on('route:main', function(listId) {
      console.assert(!page);
      if (listId !== null) {
        listId = Number(listId);
      }
      page = new Page({router: router, initialListId: listId});
      React.renderComponent(page, document.getElementById('c'));
    });

    Backbone.history.start({pushState: true});
  };
}());
