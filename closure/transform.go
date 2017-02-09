// Package closure provides closure transform for GCIL representation.
//
// Closure transform is a process to move all functions to toplevel of program.
// If a function does not contain any free variables, it can be moved to toplevel simply.
// But when containing any free variables, the function must take a closure struct as
// hidden parameter. And need to insert a code to make a closure at the definition
// point of the function.
//
// In closure transform, it visits function's body assuming the function is a normal function.
// As the result of the visit, if some free variables found, it means that the function
// is actually not a normal function, but a closure. So restore the state and retry
// visiting its body after adding the function to closures list.
//
// Note that applied normal functions are not free variables, but applied closures are
// free variables. Normal function is not a value but closure is a value.
// So, considering recursive functions, before visiting function's body, the function must
// be determined to normal function or closure. That's the reason to assume function is a
// normal function at first and then backtrack after if needed.
package closure

import (
	"fmt"
	"github.com/rhysd/gocaml/gcil"
)

type nameSet map[string]struct{}

func (set nameSet) toArray() []string {
	ns := make([]string, 0, len(set))
	for n := range set {
		ns = append(ns, n)
	}
	return ns
}

// Do closure transform with known functions optimization
type transformWithKFO struct {
	knownFuns            nameSet
	replacedFuns         map[*gcil.Insn]*gcil.MakeCls // nil means simply removing the function
	closureCalls         []*gcil.App
	closures             map[string][]string // Mapping function name to free variables
	closureBlockFreeVars map[string]nameSet  // Known free variables of closures' blocks
}

func (trans *transformWithKFO) dup() *transformWithKFO {
	known := make(map[string]struct{}, len(trans.knownFuns))
	for k := range trans.knownFuns {
		known[k] = struct{}{}
	}
	funs := make(map[*gcil.Insn]*gcil.MakeCls, len(trans.replacedFuns))
	for f, v := range trans.replacedFuns {
		funs[f] = v
	}
	clss := make(map[string][]string, len(trans.closures))
	for f, fv := range trans.closures {
		clss[f] = fv
	}
	blks := make(map[string]nameSet, len(trans.closureBlockFreeVars))
	for f, fv := range trans.closureBlockFreeVars {
		blks[f] = fv
	}
	return &transformWithKFO{
		known,
		funs,
		// Need not to copy deeply because append() will make another array.
		// So append() does not break the original array.
		trans.closureCalls,
		clss,
		blks,
	}
}

func (trans *transformWithKFO) start(block *gcil.Block) {
	// Skip first NOP instruction
	trans.explore(block.Top.Next)
}

func (trans *transformWithKFO) explore(insn *gcil.Insn) {
	if insn.Next == nil {
		// Reaches bottom of the block
		return
	}

	switch val := insn.Val.(type) {
	case *gcil.Fun:
		// Assume the function is not a closure and try to transform its body
		dup := trans.dup()
		dup.knownFuns[insn.Ident] = struct{}{}
		dup.start(val.Body)
		// Check there is no free variable actually
		fv := gatherFreeVars(val.Body, dup)
		for _, p := range val.Params {
			delete(fv, p)
		}
		if len(fv) != 0 {
			// Assumed the function is not a closure. But there are actually some
			// free variables. It means that the function is actually a closure.
			// Discard 'dup' and retry visiting its body with adding it to closures.
			trans.start(val.Body)
			fv = gatherFreeVars(val.Body, trans)
			for _, p := range val.Params {
				delete(fv, p)
			}
			arr := fv.toArray()
			trans.closures[insn.Ident] = arr
		}

		// Visit recursively
		trans.explore(insn.Next)

		fv = gatherFreeVarsTillTheEnd(insn.Next, trans)
		trans.closureBlockFreeVars[insn.Ident] = fv
		var replaced *gcil.MakeCls = nil
		if _, ok := fv[insn.Ident]; ok {
			vars, ok := trans.closures[insn.Ident]
			if !ok {
				// When the function is used as a variable, it must have an empty
				// closure even if there is no free variable for the function.
				// It's because we can't know a passed function variable is a closure or not.
				vars = []string{}
			}
			// If the function is referred from somewhere, we need to  make a closure.
			replaced = &gcil.MakeCls{vars, insn.Ident}
		}
		trans.replacedFuns[insn] = replaced
	case *gcil.App:
		if _, ok := trans.knownFuns[val.Callee]; ok {
			trans.closureCalls = append(trans.closureCalls, val)
		}
		trans.explore(insn.Next)
	default:
		trans.explore(insn.Next)
	}
}

func Transform(ir *gcil.Block) *gcil.Program {
	t := &transformWithKFO{
		map[string]struct{}{},
		map[*gcil.Insn]*gcil.MakeCls{},
		[]*gcil.App{},
		map[string][]string{},
		map[string]nameSet{},
	}
	t.start(ir)

	// Modify instructions in IR
	for _, app := range t.closureCalls {
		app.Closure = true
	}
	toplevel := map[string]*gcil.Fun{}
	for insn, make := range t.replacedFuns {
		f, ok := insn.Val.(*gcil.Fun)
		if !ok {
			panic(fmt.Sprintf("Replaced function '%s' is actually not a function: %v", insn.Ident, insn.Val))
		}
		toplevel[insn.Ident] = f

		if make == nil {
			// It's not a closure. Simply remove 'fun' instruction from list
			insn.RemoveFromList()
		} else {
			// Replace 'fun' with 'makecls' to make a closure instead of defining the function
			insn.Val = make
		}
	}

	return &gcil.Program{toplevel, t.closures, ir}
}
