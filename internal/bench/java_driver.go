package bench

// javaDriver is the timing harness for a Java unit: the same literal calls as the recorder, repeated until
// the budget is spent.
//
// It needs the class and the cases, which the caller writes into a generated source file: see
// javaBenchSource.
const javaDriverHeader = `public final class Bench {
	static long sink;
	public static void main(String[] args) {
		double minMs = Double.parseDouble(args[0]);
		int repeat = Integer.parseInt(args[1]);
		StringBuilder out = new StringBuilder("[");
		boolean first = true;
`
